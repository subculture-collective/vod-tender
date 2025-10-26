{{/*
Expand the name of the chart.
*/}}
{{- define "vod-tender.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "vod-tender.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "vod-tender.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "vod-tender.labels" -}}
helm.sh/chart: {{ include "vod-tender.chart" . }}
{{ include "vod-tender.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.labels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "vod-tender.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vod-tender.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
API component labels
*/}}
{{- define "vod-tender.api.labels" -}}
{{ include "vod-tender.labels" . }}
app.kubernetes.io/component: api
{{- end }}

{{/*
API selector labels
*/}}
{{- define "vod-tender.api.selectorLabels" -}}
{{ include "vod-tender.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end }}

{{/*
Frontend component labels
*/}}
{{- define "vod-tender.frontend.labels" -}}
{{ include "vod-tender.labels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Frontend selector labels
*/}}
{{- define "vod-tender.frontend.selectorLabels" -}}
{{ include "vod-tender.selectorLabels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
Postgres component labels
*/}}
{{- define "vod-tender.postgres.labels" -}}
{{ include "vod-tender.labels" . }}
app.kubernetes.io/component: postgres
{{- end }}

{{/*
Postgres selector labels
*/}}
{{- define "vod-tender.postgres.selectorLabels" -}}
{{ include "vod-tender.selectorLabels" . }}
app.kubernetes.io/component: postgres
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "vod-tender.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "vod-tender.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Secret name
*/}}
{{- define "vod-tender.secretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- include "vod-tender.fullname" . }}-secrets
{{- end }}
{{- end }}

{{/*
Database DSN
*/}}
{{- define "vod-tender.dbDsn" -}}
{{- if .Values.postgres.enabled }}
postgres://{{ .Values.postgres.username }}:{{ .Values.secrets.postgres.password }}@{{ include "vod-tender.fullname" . }}-postgres:5432/{{ .Values.postgres.database }}?sslmode=disable
{{- else }}
{{- required "A valid external database DSN is required when postgres.enabled is false" .Values.postgres.externalDsn }}
{{- end }}
{{- end }}
