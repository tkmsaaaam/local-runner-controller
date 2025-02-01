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
				t.Errorf("imageName() = \n%v, want \n%v", actual, tt.want)
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
		{
			name:  "runner is not present",
			param: []byte(`{"limit": 1, "base_image": "Noble", "runner": {"owner": "tkmsaaaam"}}`),
			want:  want{config: nil, err: fmt.Errorf("Runner is not valid in config.json auth is required")},
		},
		{
			name:  "valid config",
			param: []byte(`{"limit": 1, "base_image": "Noble", "runner": {"owner": "tkmsaaaam", "auth": {"is_app": false, "access_token": "example_access_token"}}}`),
			want:  want{config: &Config{Limit: 1, BaseImage: "Noble", Version: "2.322.0"}, err: nil},
		},
		{
			name:  "custom config",
			param: []byte(`{"image_host": "localhost:5000", "base_image": "Noble", "runner": {"owner": "tkmsaaaam", "auth": {"is_app": false, "access_token": "example_access_token"}}}`),
			want:  want{config: &Config{Limit: 2, BaseImage: "Noble", ImageHost: "localhost:5000", Version: "2.322.0"}, err: nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			actualConfig, actualError := makeConfig(tt.param)

			if actualConfig != nil && tt.want.config != nil {
				if actualConfig.Limit != tt.want.config.Limit {
					t.Errorf("makeConfig() config.Limit = \n%v, want \n%v", actualConfig, tt.want.config)
				}
				if actualConfig.BaseImage != tt.want.config.BaseImage {
					t.Errorf("makeConfig() config.BaseImage = \n%v, want \n%v", actualConfig, tt.want.config)
				}
				if actualConfig.ImageHost != tt.want.config.ImageHost {
					t.Errorf("makeConfig() config.ImageHost = \n%v, want \n%v", actualConfig, tt.want.config)
				}
				if actualConfig.Version != tt.want.config.Version {
					t.Errorf("makeConfig() config.Version = \n%v, want \n%v", actualConfig, tt.want.config)
				}
			}
			assert(t, "makeConfig() error", actualError, tt.want.err)
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

			assert(t, "runner.validate()", actual, tt.want)
		})
	}
}

func TestSetDefaultValue(t *testing.T) {
	tests := []struct {
		name  string
		param *Runner
		want  *Runner
	}{
		{
			name:  "setted",
			param: &Runner{Owner: "owner", Auth: &Auth{IsApp: false, AccessToken: "access_token"}},
			want:  &Runner{Owner: "owner", Auth: &Auth{IsApp: false, AccessToken: "access_token"}, ApiDomain: "api.github.com", Domain: "github.com"},
		},
		{
			name:  "not overwritten",
			param: &Runner{Owner: "owner", Auth: &Auth{IsApp: false, AccessToken: "access_token"}, ApiDomain: "api.example.com", Domain: "example.com"},
			want:  &Runner{Owner: "owner", Auth: &Auth{IsApp: false, AccessToken: "access_token"}, ApiDomain: "api.example.com", Domain: "example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			tt.param.setDefaultValue()

			if tt.param.ApiDomain != tt.want.ApiDomain {
				t.Errorf("runner.ApiDomain = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Domain != tt.want.Domain {
				t.Errorf("runner.Domain = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Owner != tt.want.Owner {
				t.Errorf("runner.Owner = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Auth.IsApp != tt.want.Auth.IsApp {
				t.Errorf("runner.Auth.IsApp = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Auth.AccessToken != tt.want.Auth.AccessToken {
				t.Errorf("runner.Auth.AccessToken = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Auth.App.Id != tt.want.Auth.App.Id {
				t.Errorf("runner.Auth.App.Id = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Auth.App.InstallationId != tt.want.Auth.App.InstallationId {
				t.Errorf("runner.Auth.App.InstallationId = \n%v, want \n%v", tt.param, tt.want)
			}
			if tt.param.Auth.App.KeyPath != tt.want.Auth.App.KeyPath {
				t.Errorf("runner.Auth.App.KeyPath = \n%v, want \n%v", tt.param, tt.want)
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
			assert(t, "auth.validate()", actual, tt.want)
		})
	}
}

func assert(t *testing.T, name string, actual, want error) {
	if actual != want {
		if (actual == nil && want != nil) || (actual != nil && want == nil) || (actual.Error() != want.Error()) {
			t.Errorf("%s = \n%v, want \n%v", name, actual, want)
		}
	}
}
