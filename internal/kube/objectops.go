package kube

// The kind-specific buttons beyond scale and restart: suspending a Job or a
// CronJob, pausing a Deployment's rollout, running a CronJob now, evicting a
// pod, and answering a certificate signing request.
//
// Each is what the matching kubectl command does, sent the same way, so an
// object changed from here and one changed from the command line look the same
// to everything watching.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// specFlagPatch is the merge patch that sets one boolean under spec:
// {"spec":{"suspend":true}}.
func specFlagPatch(field string, on bool) []byte {
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{field: on}})
	return patch
}

// patchSpecFlag sets one boolean under an object's spec.
func (w *Watcher) patchSpecFlag(kc Context, kind, namespace, name, field string, on bool) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		mapping, err := c.mappingForKind(kind)
		if err != nil {
			return err
		}
		_, err = resourceFor(c.dynamic, mapping, namespace).Patch(
			ctx, name, types.MergePatchType, specFlagPatch(field, on), metav1.PatchOptions{},
		)
		return err
	})
}

// Suspend stops a CronJob from creating Jobs, or a Job from running pods, or
// lets either carry on. A suspended Job keeps what it has done: its pods are
// removed, and resuming it starts new ones.
func (w *Watcher) Suspend(kc Context, kind, namespace, name string, on bool) error {
	return w.patchSpecFlag(kc, kind, namespace, name, "suspend", on)
}

// PauseRollout stops a Deployment from acting on changes to its template, or
// lets it act on them again: `kubectl rollout pause` and `resume`. Scaling a
// paused Deployment still works; rolling it does not.
func (w *Watcher) PauseRollout(kc Context, namespace, name string, on bool) error {
	return w.patchSpecFlag(kc, KindDeployments, namespace, name, "paused", on)
}

// manualJobName names a Job run by hand from a CronJob. The controller names
// its own after the scheduled minute; this names them after the second the
// button was pressed, and keeps inside the 63 characters a Job's name may have
// once its pods' labels carry it.
func manualJobName(cronJob string, at time.Time) string {
	suffix := "-manual-" + strconv.FormatInt(at.Unix(), 10)
	const most = 63
	if len(cronJob)+len(suffix) > most {
		cronJob = cronJob[:most-len(suffix)]
	}
	return cronJob + suffix
}

// jobFromCronJob builds the Job `kubectl create job --from=cronjob/<name>`
// would: the CronJob's job template, owned by the CronJob so it is listed and
// cleaned up with the ones the schedule makes, and marked as started by hand.
func jobFromCronJob(cron *unstructured.Unstructured, name string) (*unstructured.Unstructured, error) {
	template, found, err := unstructured.NestedMap(cron.Object, "spec", "jobTemplate")
	if err != nil || !found {
		return nil, fmt.Errorf("cron job %s has no job template", cron.GetName())
	}
	spec, _, _ := unstructured.NestedMap(template, "spec")
	labels, _, _ := unstructured.NestedStringMap(template, "metadata", "labels")
	annotations, _, _ := unstructured.NestedStringMap(template, "metadata", "annotations")
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["cronjob.kubernetes.io/instantiate"] = "manual"

	job := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"spec":       spec,
	}}
	job.SetName(name)
	job.SetNamespace(cron.GetNamespace())
	job.SetLabels(labels)
	job.SetAnnotations(annotations)
	controller := true
	job.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: cron.GetAPIVersion(),
		Kind:       cron.GetKind(),
		Name:       cron.GetName(),
		UID:        cron.GetUID(),
		Controller: &controller,
	}})
	return job, nil
}

// TriggerCronJob runs a CronJob now, outside its schedule, and returns the
// name of the Job it started.
func (w *Watcher) TriggerCronJob(kc Context, namespace, name string) (string, error) {
	var created string
	err := w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		cron, _, err := c.get(ctx, KindCronJobs, namespace, name)
		if err != nil {
			return err
		}
		job, err := jobFromCronJob(cron, manualJobName(name, time.Now()))
		if err != nil {
			return err
		}
		mapping, err := c.mappingForKind(KindJobs)
		if err != nil {
			return err
		}
		got, err := resourceFor(c.dynamic, mapping, namespace).Create(ctx, job, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		created = got.GetName()
		return nil
	})
	return created, err
}

// Evict asks for one pod to be moved, through the same eviction API a drain
// uses. Unlike a delete it is refused when a disruption budget would be broken,
// and the refusal says so.
func (w *Watcher) Evict(kc Context, namespace, name string) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		return c.evictPod(ctx, PodRef{Namespace: namespace, Name: name}, nil)
	})
}

// csrAnswer is the condition `kubectl certificate approve` or `deny` adds to a
// request. Returned rather than applied so what is written can be checked
// without a cluster.
func csrAnswer(approve bool, now time.Time) certificatesv1.CertificateSigningRequestCondition {
	answer := certificatesv1.CertificateSigningRequestCondition{
		Type:           certificatesv1.CertificateApproved,
		Status:         corev1.ConditionTrue,
		Reason:         "K8sDocksideApprove",
		Message:        "This CSR was approved by K8s Dockside",
		LastUpdateTime: metav1.NewTime(now),
	}
	if !approve {
		answer.Type = certificatesv1.CertificateDenied
		answer.Reason = "K8sDocksideDeny"
		answer.Message = "This CSR was denied by K8s Dockside"
	}
	return answer
}

// AnswerCSR approves or denies a certificate signing request. A request that
// has already been answered either way is left alone: the API server would
// refuse a request that is both, and asking twice is a mistake worth naming.
func (w *Watcher) AnswerCSR(kc Context, name string, approve bool) error {
	return w.withClient(kc, func(c *clusterClient) error {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		csrs := c.typed.CertificatesV1().CertificateSigningRequests()
		csr, err := csrs.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for _, cond := range csr.Status.Conditions {
			if cond.Type == certificatesv1.CertificateApproved || cond.Type == certificatesv1.CertificateDenied {
				return fmt.Errorf("certificate signing request %s is already %s", name, cond.Type)
			}
		}
		csr.Status.Conditions = append(csr.Status.Conditions, csrAnswer(approve, time.Now()))
		_, err = csrs.UpdateApproval(ctx, name, csr, metav1.UpdateOptions{})
		return err
	})
}
