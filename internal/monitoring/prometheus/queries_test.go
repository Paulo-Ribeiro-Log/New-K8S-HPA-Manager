package prometheus

import (
	"strings"
	"testing"
)

// TestQueryBuilder testa o builder de queries
func TestQueryBuilder(t *testing.T) {
	tests := []struct {
		name      string
		template  QueryTemplate
		vars      map[string]string
		wantErr   bool
		wantQuery string
	}{
		{
			name:     "CPU Usage Query",
			template: CPUUsageQuery,
			vars: map[string]string{
				"namespace":    "production",
				"pod_selector": "my-app.*",
			},
			wantErr:   false,
			wantQuery: "production",
		},
		{
			name:     "Memory Usage Query",
			template: MemoryUsageQuery,
			vars: map[string]string{
				"namespace":    "staging",
				"pod_selector": "test-app.*",
			},
			wantErr:   false,
			wantQuery: "staging",
		},
		{
			name:     "Missing Variables",
			template: CPUUsageQuery,
			vars:     map[string]string{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewQueryBuilder(tt.template)

			// Aplica variáveis
			for key, value := range tt.vars {
				builder.vars[key] = value
			}

			// Build
			query, err := builder.Build()

			if (err != nil) != tt.wantErr {
				t.Errorf("Build() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if query == "" {
					t.Error("Expected non-empty query")
				}

				// Verifica se contém as variáveis esperadas
				if !strings.Contains(query, tt.wantQuery) {
					t.Errorf("Query does not contain expected value: %s", tt.wantQuery)
				}

				// Verifica que não há placeholders não substituídos
				if strings.Contains(query, "{{.") {
					t.Error("Query contains unsubstituted placeholders")
				}
			}
		})
	}
}

// TestQueryBuilderMethods testa os métodos fluent
func TestQueryBuilderMethods(t *testing.T) {
	builder := NewQueryBuilder(CPUUsageQuery).
		WithNamespace("production").
		WithHPAName("my-app")

	query, err := builder.Build()

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if !strings.Contains(query, "production") {
		t.Error("Query does not contain namespace")
	}

	if !strings.Contains(query, "my-app") {
		t.Error("Query does not contain HPA name")
	}

	// Verifica que pod_selector foi automaticamente definido
	if !strings.Contains(query, "my-app.*") {
		t.Error("Query does not contain pod selector")
	}
}

// TestBuildHelperFunctions testa as funções helper
func TestBuildHelperFunctions(t *testing.T) {
	tests := []struct {
		name     string
		buildFn  func(string, string) string
		arg1     string
		arg2     string
		contains []string
	}{
		{
			name:     "Build CPU Query",
			buildFn:  BuildCPUQuery,
			arg1:     "production",
			arg2:     "my-app",
			contains: []string{"production", "my-app", "cpu"},
		},
		{
			name:     "Build Memory Query",
			buildFn:  BuildMemoryQuery,
			arg1:     "staging",
			arg2:     "test-app",
			contains: []string{"staging", "test-app", "memory"},
		},
		{
			name:     "Build Request Rate Query",
			buildFn:  BuildRequestRateQuery,
			arg1:     "production",
			arg2:     "api-service",
			contains: []string{"production", "api-service", "http_requests_total"},
		},
		{
			name:     "Build Error Rate Query",
			buildFn:  BuildErrorRateQuery,
			arg1:     "production",
			arg2:     "api-service",
			contains: []string{"production", "api-service", "5.."},
		},
		{
			name:     "Build P95 Latency Query",
			buildFn:  BuildP95LatencyQuery,
			arg1:     "production",
			arg2:     "api-service",
			contains: []string{"production", "api-service", "0.95", "histogram_quantile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := tt.buildFn(tt.arg1, tt.arg2)

			if query == "" {
				t.Error("Expected non-empty query")
			}

			for _, expected := range tt.contains {
				if !strings.Contains(query, expected) {
					t.Errorf("Query does not contain expected value: %s", expected)
				}
			}

			// Verifica que não há placeholders
			if strings.Contains(query, "{{.") {
				t.Error("Query contains unsubstituted placeholders")
			}
		})
	}
}

// TestGetAllTemplates testa GetAllTemplates
func TestGetAllTemplates(t *testing.T) {
	templates := GetAllTemplates()

	if len(templates) == 0 {
		t.Error("Expected at least one template")
	}

	// Verifica que cada template tem os campos necessários
	for _, tmpl := range templates {
		if tmpl.Name == "" {
			t.Error("Template without name")
		}

		if tmpl.Description == "" {
			t.Error("Template without description")
		}

		if tmpl.Query == "" {
			t.Error("Template without query")
		}

		if len(tmpl.Variables) == 0 {
			t.Logf("Warning: Template %s has no variables defined", tmpl.Name)
		}
	}

	// Verifica templates específicos
	expectedTemplates := map[string]bool{
		"cpu_usage":           false,
		"memory_usage":        false,
		"request_rate":        false,
		"error_rate":          false,
		"p95_latency":         false,
		"hpa_current_replicas": false,
	}

	for _, tmpl := range templates {
		if _, exists := expectedTemplates[tmpl.Name]; exists {
			expectedTemplates[tmpl.Name] = true
		}
	}

	for name, found := range expectedTemplates {
		if !found {
			t.Errorf("Expected template %s not found", name)
		}
	}
}

// TestQueryTemplateVariables testa que templates definem variáveis corretas
func TestQueryTemplateVariables(t *testing.T) {
	tests := []struct {
		template      QueryTemplate
		requiredVars  []string
	}{
		{
			template:     CPUUsageQuery,
			requiredVars: []string{"namespace", "pod_selector"},
		},
		{
			template:     MemoryUsageQuery,
			requiredVars: []string{"namespace", "pod_selector"},
		},
		{
			template:     HPACurrentReplicasQuery,
			requiredVars: []string{"namespace", "hpa_name"},
		},
		{
			template:     RequestRateQuery,
			requiredVars: []string{"namespace", "service"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.template.Name, func(t *testing.T) {
			// Verifica se a query contém os placeholders esperados
			for _, varName := range tt.requiredVars {
				placeholder := "{{." + varName + "}}"
				if !strings.Contains(tt.template.Query, placeholder) {
					t.Errorf("Query does not contain placeholder %s", placeholder)
				}
			}
		})
	}
}

// BenchmarkQueryBuilder benchmark para query builder
func BenchmarkQueryBuilder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = NewQueryBuilder(CPUUsageQuery).
			WithNamespace("production").
			WithHPAName("my-app").
			Build()
	}
}

// BenchmarkBuildCPUQuery benchmark para BuildCPUQuery
func BenchmarkBuildCPUQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = BuildCPUQuery("production", "my-app")
	}
}
