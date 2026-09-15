{{/*
Chart name, overridable with nameOverride.
*/}}
{{- define "k8sdockside.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name, truncated to 63 characters because some Kubernetes
name fields are limited to that (by the DNS naming spec). If the release name
already contains the chart name it is used as is.
*/}}
{{- define "k8sdockside.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version, as used by the chart label.
*/}}
{{- define "k8sdockside.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "k8sdockside.labels" -}}
helm.sh/chart: {{ include "k8sdockside.chart" . }}
{{ include "k8sdockside.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "k8sdockside.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8sdockside.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Name of the ServiceAccount to use.
*/}}
{{- define "k8sdockside.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "k8sdockside.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Container image reference: repository@digest when a digest is set, otherwise
repository:tag with the tag defaulting to the chart's appVersion.
*/}}
{{- define "k8sdockside.image" -}}
{{- if .Values.image.digest }}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else }}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion | toString) }}
{{- end }}
{{- end }}

{{/*
Names of the Secrets the chart generates.
*/}}
{{- define "k8sdockside.kubeconfigsSecretName" -}}
{{- printf "%s-kubeconfigs" (include "k8sdockside.fullname" .) }}
{{- end }}

{{- define "k8sdockside.authSecretName" -}}
{{- printf "%s-auth" (include "k8sdockside.fullname" .) }}
{{- end }}

{{/*
"true" when any kubeconfig is mounted -- inline or from an existing Secret --
and empty otherwise.
*/}}
{{- define "k8sdockside.hasKubeconfigs" -}}
{{- if or .Values.clusters.kubeconfigs .Values.clusters.existingSecrets -}}
true
{{- end -}}
{{- end }}

{{/*
"true" when a providers file is mounted, and empty otherwise.
*/}}
{{- define "k8sdockside.hasProviders" -}}
{{- if or .Values.auth.providers .Values.auth.existingProvidersSecret.name -}}
true
{{- end -}}
{{- end }}

{{/*
Where the bootstrap admin comes from: "existing" (a Secret named in values),
"inline" (username and password in values, written to the chart's Secret) or
empty for none.
*/}}
{{- define "k8sdockside.bootstrapAdminMode" -}}
{{- $b := .Values.auth.bootstrapAdmin -}}
{{- if $b.existingSecret.name -}}
existing
{{- else if or $b.username $b.password -}}
{{- if not (and $b.username $b.password) -}}
{{- fail "auth.bootstrapAdmin: set both username and password, or existingSecret.name" -}}
{{- end -}}
inline
{{- end -}}
{{- end }}

{{/*
The public URL: auth.publicURL when set, else derived from the first ingress
host (https when that host is covered by ingress.tls), else empty.
*/}}
{{- define "k8sdockside.publicURL" -}}
{{- if .Values.auth.publicURL -}}
{{- .Values.auth.publicURL | trimSuffix "/" -}}
{{- else if and .Values.ingress.enabled .Values.ingress.hosts -}}
{{- $host := (first .Values.ingress.hosts).host -}}
{{- if $host -}}
{{- $scheme := "http" -}}
{{- range .Values.ingress.tls -}}
{{- if has $host .hosts -}}
{{- $scheme = "https" -}}
{{- end -}}
{{- end -}}
{{- printf "%s://%s" $scheme $host -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
auth.providers as the JSON array the server reads from
K8SDOCKSIDE_AUTH_PROVIDERS_FILE, after checking the fields a provider cannot
work without.
*/}}
{{- define "k8sdockside.providersJSON" -}}
{{- $types := list "github" "google" "facebook" "gitlab" "microsoft" "oidc" -}}
{{- range $i, $p := .Values.auth.providers -}}
{{- if not $p.id -}}
{{- fail (printf "auth.providers[%d]: id is required" $i) -}}
{{- end -}}
{{- if not (regexMatch "^[a-z0-9][a-z0-9-]{0,31}$" (toString $p.id)) -}}
{{- fail (printf "auth.providers[%d]: id %q must be lowercase letters, digits and '-', starting with a letter or digit, at most 32 characters: it becomes part of the callback URL" $i (toString $p.id)) -}}
{{- end -}}
{{- if not (has $p.type $types) -}}
{{- fail (printf "auth.providers[%d] (%s): type must be one of %s" $i $p.id (join ", " $types)) -}}
{{- end -}}
{{- if not $p.clientId -}}
{{- fail (printf "auth.providers[%d] (%s): clientId is required" $i $p.id) -}}
{{- end -}}
{{- if and (eq $p.type "oidc") (not $p.issuer) -}}
{{- fail (printf "auth.providers[%d] (%s): type oidc needs an issuer" $i $p.id) -}}
{{- end -}}
{{- end -}}
{{- toJson .Values.auth.providers -}}
{{- end }}

{{/*
A ClusterRoleBinding of the chart's ServiceAccount.
Takes a dict: root (the top-level context), name, role.
*/}}
{{- define "k8sdockside.clusterRoleBinding" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ .name }}
  labels:
    {{- include "k8sdockside.labels" .root | nindent 4 }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ .role }}
subjects:
  - kind: ServiceAccount
    name: {{ include "k8sdockside.serviceAccountName" .root }}
    namespace: {{ .root.Release.Namespace }}
{{- end }}
