package kube

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSpecFlagPatchSetsOneField(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal(specFlagPatch("suspend", true), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	spec := got["spec"].(map[string]any)
	if len(spec) != 1 || spec["suspend"] != true {
		t.Errorf("patch = %v, want spec.suspend=true and nothing else", got)
	}
}

func TestStateOfReadsSuspendPausedAndPending(t *testing.T) {
	job := &unstructured.Unstructured{Object: map[string]any{
		"kind": "CronJob", "spec": map[string]any{"suspend": true},
	}}
	if !stateOf(job).Suspended {
		t.Error("a suspended cron job does not read as suspended")
	}

	deployment := &unstructured.Unstructured{Object: map[string]any{
		"kind": "Deployment", "spec": map[string]any{"paused": true, "replicas": int64(1)},
	}}
	if !stateOf(deployment).Paused {
		t.Error("a paused deployment does not read as paused")
	}

	csr := &unstructured.Unstructured{Object: map[string]any{"kind": "CertificateSigningRequest"}}
	if !stateOf(csr).Pending {
		t.Error("an unanswered signing request does not read as pending")
	}
	// Pending means nothing for anything else, and must not light up the
	// approve buttons on a pod that happens to have no conditions.
	pod := &unstructured.Unstructured{Object: map[string]any{"kind": "Pod"}}
	if stateOf(pod).Pending {
		t.Error("a pod reads as a pending signing request")
	}
}

func TestManualJobNamesFitAJobName(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	if got := manualJobName("backup", at); got != "backup-manual-1800000000" {
		t.Errorf("manualJobName = %q", got)
	}
	long := manualJobName(strings.Repeat("x", 80), at)
	if len(long) != 63 || !strings.HasSuffix(long, "-manual-1800000000") {
		t.Errorf("a long name came out as %q (%d characters)", long, len(long))
	}
}

func TestJobFromCronJobCopiesTheTemplateAndOwnsIt(t *testing.T) {
	cron := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata":   map[string]any{"name": "backup", "namespace": "ops", "uid": "cron-uid"},
		"spec": map[string]any{
			"schedule": "0 3 * * *",
			"jobTemplate": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "backup"}},
				"spec": map[string]any{
					"backoffLimit": int64(2),
					"template": map[string]any{"spec": map[string]any{
						"containers": []any{map[string]any{"name": "run", "image": "backup:1"}},
					}},
				},
			},
		},
	}}

	job, err := jobFromCronJob(cron, "backup-manual-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.GetKind() != "Job" || job.GetName() != "backup-manual-1" || job.GetNamespace() != "ops" {
		t.Errorf("job identity = %s %s/%s", job.GetKind(), job.GetNamespace(), job.GetName())
	}
	if job.GetLabels()["app"] != "backup" {
		t.Errorf("labels = %v, want the template's", job.GetLabels())
	}
	// The same mark kubectl leaves, so a job run by hand can be told apart.
	if job.GetAnnotations()["cronjob.kubernetes.io/instantiate"] != "manual" {
		t.Errorf("annotations = %v, want the manual mark", job.GetAnnotations())
	}
	owners := job.GetOwnerReferences()
	if len(owners) != 1 || owners[0].UID != "cron-uid" || owners[0].Controller == nil || !*owners[0].Controller {
		t.Errorf("owners = %+v, want the cron job as controller", owners)
	}
	if n, _, _ := unstructured.NestedInt64(job.Object, "spec", "backoffLimit"); n != 2 {
		t.Errorf("spec.backoffLimit = %d, want the template's 2", n)
	}
}

func TestJobFromCronJobWithoutATemplateIsRefused(t *testing.T) {
	cron := &unstructured.Unstructured{Object: map[string]any{"kind": "CronJob", "spec": map[string]any{}}}
	if _, err := jobFromCronJob(cron, "x"); err == nil {
		t.Error("a cron job with no template made a job")
	}
}

func TestCSRAnswerIsTheConditionKubectlAdds(t *testing.T) {
	now := time.Now()
	approve := csrAnswer(true, now)
	if approve.Type != certificatesv1.CertificateApproved || approve.Status != "True" || approve.Reason == "" {
		t.Errorf("approve = %+v", approve)
	}
	deny := csrAnswer(false, now)
	if deny.Type != certificatesv1.CertificateDenied || deny.Status != "True" {
		t.Errorf("deny = %+v", deny)
	}
}
