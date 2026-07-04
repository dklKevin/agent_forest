package main

import (
	"testing"

	"github.com/dklKevin/agentforest/internal/app"
	"github.com/dklKevin/agentforest/internal/store"
)

func TestShouldStampLastOpenedOnExit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		demoFlag bool
		app      *app.App
		want     bool
	}{
		{
			name:     "explicit demo with connected forest",
			demoFlag: true,
			app:      connectedApp(),
			want:     false,
		},
		{
			name:     "normal launch with connected forest",
			demoFlag: false,
			app:      connectedApp(),
			want:     true,
		},
		{
			name:     "normal launch still unconnected",
			demoFlag: false,
			app:      &app.App{Settings: &store.Settings{}},
			want:     false,
		},
		{
			name:     "missing app",
			demoFlag: false,
			app:      nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := shouldStampLastOpenedOnExit(tt.demoFlag, tt.app); got != tt.want {
				t.Fatalf("shouldStampLastOpenedOnExit(%v) = %v, want %v", tt.demoFlag, got, tt.want)
			}
		})
	}
}

func connectedApp() *app.App {
	return &app.App{Settings: &store.Settings{Roots: []string{"/repo"}}}
}
