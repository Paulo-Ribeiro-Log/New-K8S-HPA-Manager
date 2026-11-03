package appremoval

import (
	"encoding/json"
	"fmt"
	"strings"
)

type rawList struct {
	Items []rawItem `json:"items"`
}

// rawItem não declara `data`/`stringData`: o conteúdo de Secret é descartado no parse.
type rawItem struct {
	Kind     string `json:"kind"`
	Type     string `json:"type"`
	Metadata struct {
		Name            string            `json:"name"`
		UID             string            `json:"uid"`
		Labels          map[string]string `json:"labels"`
		Annotations     map[string]string `json:"annotations"`
		DeletionTS      string            `json:"deletionTimestamp"`
		Finalizers      []string          `json:"finalizers"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
			UID  string `json:"uid"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec     json.RawMessage `json:"spec"`
	Subjects []struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"subjects"`
	RoleRef struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"roleRef"`
}

// ParseList converte a saída de `kubectl get <kinds> -o json` em Resources.
func ParseList(data []byte) ([]Resource, error) {
	var list rawList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("falha ao parsear lista do kubectl: %w", err)
	}
	out := make([]Resource, 0, len(list.Items))
	for _, it := range list.Items {
		r := Resource{
			Kind:              it.Kind,
			Name:              it.Metadata.Name,
			UID:               it.Metadata.UID,
			Type:              it.Type,
			Labels:            it.Metadata.Labels,
			Annotations:       it.Metadata.Annotations,
			Spec:              it.Spec,
			DeletionTimestamp: it.Metadata.DeletionTS,
			Finalizers:        it.Metadata.Finalizers,
			RoleRefKind:       it.RoleRef.Kind,
			RoleRefName:       it.RoleRef.Name,
		}
		for _, o := range it.Metadata.OwnerReferences {
			r.Owners = append(r.Owners, Owner{Kind: o.Kind, Name: o.Name, UID: o.UID})
		}
		for _, s := range it.Subjects {
			r.Subjects = append(r.Subjects, Subject{Kind: s.Kind, Name: s.Name, Namespace: s.Namespace})
		}
		out = append(out, r)
	}
	return out, nil
}

// ─── Decodificação de spec por kind ─────────────────────────────────────────

type nameRef struct {
	Name string `json:"name"`
}

type podSpec struct {
	ServiceAccountName string `json:"serviceAccountName"`
	Volumes            []struct {
		ConfigMap *nameRef `json:"configMap"`
		Secret    *struct {
			SecretName string `json:"secretName"`
		} `json:"secret"`
		PVC *struct {
			ClaimName string `json:"claimName"`
		} `json:"persistentVolumeClaim"`
		Projected *struct {
			Sources []struct {
				ConfigMap *nameRef `json:"configMap"`
				Secret    *nameRef `json:"secret"`
			} `json:"sources"`
		} `json:"projected"`
	} `json:"volumes"`
	Containers     []containerSpec `json:"containers"`
	InitContainers []containerSpec `json:"initContainers"`
	ImagePull      []nameRef       `json:"imagePullSecrets"`
}

type containerSpec struct {
	EnvFrom []struct {
		ConfigMapRef *nameRef `json:"configMapRef"`
		SecretRef    *nameRef `json:"secretRef"`
	} `json:"envFrom"`
	Env []struct {
		ValueFrom *struct {
			ConfigMapKeyRef *nameRef `json:"configMapKeyRef"`
			SecretKeyRef    *nameRef `json:"secretKeyRef"`
		} `json:"valueFrom"`
	} `json:"env"`
}

type podTemplate struct {
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec podSpec `json:"spec"`
}

type workloadSpec struct {
	Template    *podTemplate `json:"template"`
	JobTemplate *struct {
		Spec struct {
			Template podTemplate `json:"template"`
		} `json:"spec"`
	} `json:"jobTemplate"`
	VolumeClaimTemplates []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"volumeClaimTemplates"`
}

// workload é a visão de um workload já decodificado: labels dos pods e o que eles referenciam.
type workload struct {
	res       *Resource
	podLabels map[string]string
	refs      refs
	vctNames  []string
}

type refs struct {
	configMaps []string
	secrets    []string
	pvcs       []string
	sa         string
}

func isWorkloadKind(kind string) bool {
	switch kind {
	case KindDeployment, KindStatefulSet, KindDaemonSet, KindCronJob:
		return true
	}
	return false
}

func decodeWorkload(r *Resource) workload {
	w := workload{res: r}
	var spec workloadSpec
	if err := json.Unmarshal(r.Spec, &spec); err != nil {
		return w
	}
	var tpl *podTemplate
	switch {
	case spec.Template != nil:
		tpl = spec.Template
	case spec.JobTemplate != nil:
		tpl = &spec.JobTemplate.Spec.Template
	default:
		return w
	}
	w.podLabels = tpl.Metadata.Labels
	w.refs = podRefs(tpl.Spec)
	for _, v := range spec.VolumeClaimTemplates {
		w.vctNames = append(w.vctNames, v.Metadata.Name)
	}
	return w
}

func podRefs(ps podSpec) refs {
	var rf refs
	addCM := func(n string) {
		if n != "" {
			rf.configMaps = append(rf.configMaps, n)
		}
	}
	addSecret := func(n string) {
		if n != "" {
			rf.secrets = append(rf.secrets, n)
		}
	}
	for _, v := range ps.Volumes {
		if v.ConfigMap != nil {
			addCM(v.ConfigMap.Name)
		}
		if v.Secret != nil {
			addSecret(v.Secret.SecretName)
		}
		if v.PVC != nil && v.PVC.ClaimName != "" {
			rf.pvcs = append(rf.pvcs, v.PVC.ClaimName)
		}
		if v.Projected != nil {
			for _, s := range v.Projected.Sources {
				if s.ConfigMap != nil {
					addCM(s.ConfigMap.Name)
				}
				if s.Secret != nil {
					addSecret(s.Secret.Name)
				}
			}
		}
	}
	for _, c := range append(append([]containerSpec{}, ps.Containers...), ps.InitContainers...) {
		for _, e := range c.EnvFrom {
			if e.ConfigMapRef != nil {
				addCM(e.ConfigMapRef.Name)
			}
			if e.SecretRef != nil {
				addSecret(e.SecretRef.Name)
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if e.ValueFrom.ConfigMapKeyRef != nil {
				addCM(e.ValueFrom.ConfigMapKeyRef.Name)
			}
			if e.ValueFrom.SecretKeyRef != nil {
				addSecret(e.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	for _, s := range ps.ImagePull {
		addSecret(s.Name)
	}
	if ps.ServiceAccountName != defaultSAName {
		rf.sa = ps.ServiceAccountName
	}
	return rf
}

func serviceSelector(r *Resource) map[string]string {
	var spec struct {
		Selector map[string]string `json:"selector"`
	}
	_ = json.Unmarshal(r.Spec, &spec)
	return spec.Selector
}

// matchLabelsOf lê spec.<field>.matchLabels (selector, podSelector).
func matchLabelsOf(r *Resource, field string) map[string]string {
	var spec map[string]json.RawMessage
	if json.Unmarshal(r.Spec, &spec) != nil {
		return nil
	}
	var sel struct {
		MatchLabels map[string]string `json:"matchLabels"`
	}
	_ = json.Unmarshal(spec[field], &sel)
	return sel.MatchLabels
}

// backendServices devolve os nomes de Service para onde Ingress/HTTPRoute/VirtualService encaminham.
func backendServices(r *Resource) []string {
	var names []string
	switch r.Kind {
	case KindIngress:
		var spec struct {
			DefaultBackend *struct {
				Service *nameRef `json:"service"`
			} `json:"defaultBackend"`
			Rules []struct {
				HTTP *struct {
					Paths []struct {
						Backend struct {
							Service *nameRef `json:"service"`
						} `json:"backend"`
					} `json:"paths"`
				} `json:"http"`
			} `json:"rules"`
		}
		_ = json.Unmarshal(r.Spec, &spec)
		if spec.DefaultBackend != nil && spec.DefaultBackend.Service != nil {
			names = append(names, spec.DefaultBackend.Service.Name)
		}
		for _, rule := range spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, p := range rule.HTTP.Paths {
				if p.Backend.Service != nil {
					names = append(names, p.Backend.Service.Name)
				}
			}
		}
	case KindHTTPRoute:
		var spec struct {
			Rules []struct {
				BackendRefs []nameRef `json:"backendRefs"`
			} `json:"rules"`
		}
		_ = json.Unmarshal(r.Spec, &spec)
		for _, rule := range spec.Rules {
			for _, b := range rule.BackendRefs {
				names = append(names, b.Name)
			}
		}
	case KindVirtualService:
		var spec struct {
			HTTP []struct {
				Route []struct {
					Destination struct {
						Host string `json:"host"`
					} `json:"destination"`
				} `json:"route"`
			} `json:"http"`
		}
		_ = json.Unmarshal(r.Spec, &spec)
		for _, h := range spec.HTTP {
			for _, rt := range h.Route {
				host := rt.Destination.Host
				if host == "" {
					continue
				}
				names = append(names, strings.SplitN(host, ".", 2)[0])
			}
		}
	}
	return names
}

func hpaTarget(r *Resource) (kind, name string) {
	var spec struct {
		ScaleTargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"scaleTargetRef"`
	}
	_ = json.Unmarshal(r.Spec, &spec)
	return spec.ScaleTargetRef.Kind, spec.ScaleTargetRef.Name
}
