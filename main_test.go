package main

import (
	"fmt"
	"testing"
)

func TestImageName(t *testing.T) {
	tests := []struct {
		name  string
		param *Config
		want  string
	}{
		{
			name:  "empty image host",
			param: &Config{ImageHost: "", BaseImage: "ubuntu", Version: "2.322.0"},
			want:  "local-runner:ubuntu-2.322.0",
		},
		{
			name:  "with image host",
			param: &Config{ImageHost: "localhost:5000", BaseImage: "ubuntu", Version: "2.322.0"},
			want:  "localhost:5000/local-runner:ubuntu-2.322.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			actual := tt.param.imageName()

			if actual != tt.want {
				t.Errorf("imageName() = %v, want %v", actual, tt.want)
			}
		})
	}
}

func TestMakeConfig(t *testing.T) {
	type want struct {
		config *Config
		err    error
	}

	tests := []struct {
		name  string
		param []byte
		want  want
	}{
		{
			name:  "invalid json",
			param: []byte(`{`),
			want:  want{config: nil, err: fmt.Errorf("Config file (config.json) is invalid.")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			actualConfig, actualError := makeConfig(tt.param)

			if actualConfig != tt.want.config {
				t.Errorf("makeConfig() config = %v, want %v", actualConfig, tt.want.config)
			}
			if (actualError == nil && tt.want.err != nil) || (actualError != nil && tt.want.err == nil) || (actualError.Error() != tt.want.err.Error()) {
				t.Errorf("makeConfig() error = %v, want %v", actualError, tt.want.err)
			}
		})
	}
}

func TestRunnerValidate(t *testing.T) {
	tests := []struct {
		name  string
		param *Runner
		want  error
	}{
		{
			name:  "Owner is empty",
			param: &Runner{Owner: "", Repository: "repo"},
			want:  fmt.Errorf("owner is required"),
		},
		{
			name:  "auth is invalid",
			param: &Runner{Owner: "owner", Repository: "repository", Auth: &Auth{IsApp: false, AccessToken: "", App: App{KeyPath: "/path", Id: 1, InstallationId: 1}}},
			want:  fmt.Errorf("auth is invalid access_token is required"),
		},
		{
			name:  "auth is nil",
			param: &Runner{Owner: "owner"},
			want:  fmt.Errorf("auth is required"),
		},
		{
			name:  "valid",
			param: &Runner{Owner: "owner", Auth: &Auth{IsApp: false, AccessToken: "access_token"}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			actual := tt.param.validate()

			if actual != tt.want {
				if (actual == nil && tt.want != nil) || (actual != nil && tt.want == nil) || (actual.Error() != tt.want.Error()) {
					t.Errorf("runner.validate() = %v, want %v", actual, tt.want)
				}
			}
		})
	}
}

func TestAuthValidate(t *testing.T) {
	tests := []struct {
		name  string
		param *Auth
		want  error
	}{
		{
			name:  "isNotApp but AccessToken is empty",
			param: &Auth{IsApp: false, AccessToken: "", App: App{KeyPath: "/path", Id: 1, InstallationId: 1}},
			want:  fmt.Errorf("access_token is required"),
		},
		{
			name:  "isApp but KeyPath is empty",
			param: &Auth{IsApp: true, AccessToken: "AccessToken", App: App{KeyPath: "", Id: 1, InstallationId: 1}},
			want:  fmt.Errorf("key_path is required"),
		},
		{
			name:  "isApp but Id is 0",
			param: &Auth{IsApp: true, AccessToken: "AccessToken", App: App{KeyPath: "/path", Id: 0, InstallationId: 1}},
			want:  fmt.Errorf("app.id is required"),
		},
		{
			name:  "isApp but InstallationId is 0",
			param: &Auth{IsApp: true, AccessToken: "AccessToken", App: App{KeyPath: "/path", Id: 1, InstallationId: 0}},
			want:  fmt.Errorf("app.installation_id is required"),
		},
		{
			name:  "valid AccessToken",
			param: &Auth{IsApp: false, AccessToken: "AccessToken", App: App{KeyPath: "/path", Id: 1, InstallationId: 1}},
			want:  nil,
		},
		{
			name:  "valid isApp",
			param: &Auth{IsApp: true, AccessToken: "", App: App{KeyPath: "/path", Id: 1, InstallationId: 1}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			actual := tt.param.validate()

			if actual != tt.want {
				if (actual == nil && tt.want != nil) || (actual != nil && tt.want == nil) || (actual.Error() != tt.want.Error()) {
					t.Errorf("auth.validate() = %v, want %v", actual, tt.want)
				}
			}
		})
	}
}
