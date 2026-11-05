package config

import "testing"

func TestLoadPort(t *testing.T) {
	tests := []struct {
		name           string
		aspNetCorePort string
		port           string
		wantPort       string
		wantAddr       string
	}{
		{"default when nothing is set", "", "", "8080", ":8080"},
		{"PORT overrides the default", "", "9000", "9000", ":9000"},
		{"ASPNETCORE_PORT binds loopback", "5123", "", "5123", "127.0.0.1:5123"},
		{"ASPNETCORE_PORT wins over PORT", "5123", "9000", "5123", "127.0.0.1:5123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
			t.Setenv("API_TOKEN", "test-token")
			t.Setenv("ASPNETCORE_PORT", tt.aspNetCorePort)
			t.Setenv("PORT", tt.port)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", cfg.Port, tt.wantPort)
			}
			if cfg.ListenAddr != tt.wantAddr {
				t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, tt.wantAddr)
			}
		})
	}
}
