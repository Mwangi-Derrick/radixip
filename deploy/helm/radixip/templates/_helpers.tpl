{{/*
Expand the name of the chart.
*/}}
{{- define "radixip.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "radixip.fullname" -}}
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
Create chart label.
*/}}
{{- define "radixip.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "radixip.labels" -}}
helm.sh/chart: {{ include "radixip.chart" . }}
{{ include "radixip.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels (used in Deployments and Services).
*/}}
{{- define "radixip.selectorLabels" -}}
app.kubernetes.io/name: {{ include "radixip.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Operator-specific selector labels.
*/}}
{{- define "radixip.operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "radixip.name" . }}-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end }}

{{/*
ServiceAccount name for the operator.
*/}}
{{- define "radixip.serviceAccountName" -}}
{{- if .Values.operator.serviceAccount.create }}
{{- default (printf "%s-operator" (include "radixip.fullname" .)) .Values.operator.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.operator.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Redis URL — internal (sub-chart) or external.
*/}}
{{- define "radixip.redisUrl" -}}
{{- if .Values.redis.enabled }}
{{- printf "redis://%s-redis-master:6379" .Release.Name }}
{{- else }}
{{- .Values.externalRedis.url }}
{{- end }}
{{- end }}

{{/*
Sidecar flavor: "rust" (default) or "go".
*/}}
{{- define "radixip.sidecar.flavor" -}}
{{- default "rust" .Values.sidecar.flavor }}
{{- end }}

{{/*
Sidecar image repository and tag based on flavor.
*/}}
{{- define "radixip.sidecar.image" -}}
{{- $flavor := include "radixip.sidecar.flavor" . -}}
{{- if eq $flavor "go" -}}
{{- printf "%s:%s" .Values.sidecar.images.go.repository (.Values.sidecar.images.go.tag | default .Chart.AppVersion) -}}
{{- else -}}
{{- printf "%s:%s" .Values.sidecar.images.rust.repository (.Values.sidecar.images.rust.tag | default .Chart.AppVersion) -}}
{{- end -}}
{{- end }}

{{/*
Sidecar gRPC port based on flavor.
*/}}
{{- define "radixip.sidecar.grpcPort" -}}
{{- $flavor := include "radixip.sidecar.flavor" . -}}
{{- if eq $flavor "go" -}}
{{- default 50051 .Values.sidecar.images.go.grpcPort -}}
{{- else -}}
{{- default 50052 .Values.sidecar.images.rust.grpcPort -}}
{{- end -}}
{{- end }}

{{/*
Sidecar metrics port based on flavor.
*/}}
{{- define "radixip.sidecar.metricsPort" -}}
{{- $flavor := include "radixip.sidecar.flavor" . -}}
{{- if eq $flavor "go" -}}
{{- default 9090 .Values.sidecar.images.go.metricsPort -}}
{{- else -}}
{{- default 9091 .Values.sidecar.images.rust.metricsPort -}}
{{- end -}}
{{- end }}

{{/*
Standard RadixIP sidecar container definition.
Container name MUST be 'radixip-rate-limiter' to match operator reconciler.
*/}}
{{- define "radixip.sidecarContainer" -}}
- name: radixip-rate-limiter
  image: {{ include "radixip.sidecar.image" . | quote }}
  imagePullPolicy: {{ .Values.sidecar.pullPolicy | default "IfNotPresent" }}
  env:
    - name: GRPC_PORT
      value: {{ include "radixip.sidecar.grpcPort" . | quote }}
    - name: METRICS_PORT
      value: {{ include "radixip.sidecar.metricsPort" . | quote }}
    - name: RADIXIP_CONFIG
      value: "{{ .Values.sidecar.config.mountPath }}/radixip.yaml"
    - name: REDIS_URL
      value: {{ include "radixip.redisUrl" . | quote }}
  ports:
    - name: grpc
      containerPort: {{ include "radixip.sidecar.grpcPort" . }}
      protocol: TCP
    - name: metrics
      containerPort: {{ include "radixip.sidecar.metricsPort" . }}
      protocol: TCP
  {{- with .Values.sidecar.resources }}
  resources:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- if .Values.sidecar.config.configMap.enabled }}
  volumeMounts:
    - name: radixip-config
      mountPath: {{ .Values.sidecar.config.mountPath }}
      readOnly: true
  {{- end }}
{{- end }}

{{/*
Sidecar configuration volumes.
*/}}
{{- define "radixip.sidecarVolumes" -}}
{{- if .Values.sidecar.config.configMap.enabled }}
- name: radixip-config
  configMap:
    name: {{ include "radixip.fullname" . }}-sidecar-config
{{- end }}
{{- end }}
