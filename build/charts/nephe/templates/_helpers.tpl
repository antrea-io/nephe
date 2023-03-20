{{- define "nepheImageTag" -}}
{{- if .Values.image.tag }}
{{- .Values.image.tag -}}
{{- else if eq .Chart.AppVersion "latest" }}
{{- print "latest" -}}
{{- else }}
{{- print "v" .Chart.AppVersion -}}
{{- end }}
{{- end -}}

{{- define "nepheImage" -}}
{{- print .Values.image.repository ":" (include "nepheImageTag" .) -}}
{{- end -}}
