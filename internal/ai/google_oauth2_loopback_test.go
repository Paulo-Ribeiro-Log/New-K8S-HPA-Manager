package ai

import "testing"

func TestParseWIFPoolProvider(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantPool     string
		wantProvider string
		wantOK       bool
	}{
		{
			name:         "formato curto válido",
			input:        "entraid-agentspace/entraid-federation-agentspace",
			wantPool:     "entraid-agentspace",
			wantProvider: "entraid-federation-agentspace",
			wantOK:       true,
		},
		{
			name:         "formato longo válido",
			input:        "//iam.googleapis.com/locations/global/workforcePools/entraid-agentspace/providers/entraid-federation-agentspace",
			wantPool:     "entraid-agentspace",
			wantProvider: "entraid-federation-agentspace",
			wantOK:       true,
		},
		{
			// Caso real que motivou a correção: usuário colou uma URL do Vertex AI
			// Search/Agentspace no campo, em vez de "poolID/providerID".
			name:   "URL completa deve falhar",
			input:  "https://vertexaisearch.cloud.google/home/cid/e27c3217-b2b0-4e85-8002-8b0070735c03?csesidx=1422632816",
			wantOK: false,
		},
		{
			name:   "string com espaço deve falhar",
			input:  "entraid-agentspace /entraid-federation-agentspace",
			wantOK: false,
		},
		{
			name:   "string vazia deve falhar",
			input:  "",
			wantOK: false,
		},
		{
			name:   "apenas espaços deve falhar",
			input:  "   ",
			wantOK: false,
		},
		{
			name:   "string com interrogação deve falhar",
			input:  "pool/provider?query=1",
			wantOK: false,
		},
		{
			name:   "sem barra deve falhar",
			input:  "poolonlynoprovider",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, provider, ok := ParseWIFPoolProvider(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (pool=%q provider=%q)", ok, tt.wantOK, pool, provider)
			}
			if ok {
				if pool != tt.wantPool {
					t.Errorf("pool = %q, want %q", pool, tt.wantPool)
				}
				if provider != tt.wantProvider {
					t.Errorf("provider = %q, want %q", provider, tt.wantProvider)
				}
			}
		})
	}
}
