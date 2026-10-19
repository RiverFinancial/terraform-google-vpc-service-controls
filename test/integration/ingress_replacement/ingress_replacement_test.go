// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ingress_replacement_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type resourceChange struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Index   any    `json:"index"`
	Change  struct {
		Actions []string       `json:"actions"`
		After   map[string]any `json:"after"`
	} `json:"change"`
}

type plan struct {
	ResourceChanges []resourceChange `json:"resource_changes"`
}

func terraform(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	command := exec.Command("terraform", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("terraform %v: %v\n%s", args, err, output)
	}
	return output
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func readPlan(t *testing.T, dir string, variables map[string]any) plan {
	t.Helper()
	writeJSON(t, filepath.Join(dir, "terraform.tfvars.json"), variables)
	terraform(t, dir, "plan", "-input=false", "-refresh=false", "-lock=false", "-no-color", "-out=test.plan")
	var result plan
	if err := json.Unmarshal(terraform(t, dir, "show", "-json", "test.plan"), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertActions(t *testing.T, result plan, ingressActions []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, resource := range result.ResourceChanges {
		expected := []string{"no-op"}
		switch resource.Type {
		case "google_access_context_manager_service_perimeter_ingress_policy":
			expected = ingressActions
			if !strings.HasSuffix(resource.Address, `ingress_policies["stable-rule"]`) {
				t.Fatalf("unexpected ingress address: %s", resource.Address)
			}
		case "google_access_context_manager_service_perimeter":
		default:
			t.Fatalf("unexpected resource change: %s", resource.Address)
		}
		if !reflect.DeepEqual(resource.Change.Actions, expected) {
			t.Errorf("%s: actions = %v, want %v", resource.Address, resource.Change.Actions, expected)
		}
		seen[resource.Type] = true
	}
	if len(result.ResourceChanges) != 2 || len(seen) != 2 {
		t.Fatalf("expected exactly one perimeter and one ingress policy, got %+v", result.ResourceChanges)
	}
}

func TestIngressReplacementOrder(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "map"
		if legacy {
			name = "legacy-list"
		}
		t.Run(name, func(t *testing.T) {
			// Copy the actual module, not a reimplementation of its lifecycle.
			repo, err := filepath.Abs("../../..")
			if err != nil {
				t.Fatal(err)
			}
			temp := t.TempDir()
			for _, relative := range []string{"modules/regular_service_perimeter", "test/fixtures/ingress_replacement"} {
				source := filepath.Join(repo, relative)
				target := filepath.Join(temp, relative)
				if err := os.MkdirAll(target, 0755); err != nil {
					t.Fatal(err)
				}
				files, err := filepath.Glob(filepath.Join(source, "*.tf"))
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range files {
					content, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(target, filepath.Base(file)), content, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			dir := filepath.Join(temp, "test/fixtures/ingress_replacement")
			terraform(t, dir, "init", "-backend=false", "-input=false", "-no-color")
			variables := map[string]any{"use_legacy_list": legacy}
			seed := readPlan(t, dir, variables)
			instances := []any{}
			for _, resource := range seed.ResourceChanges {
				attributes := resource.Change.After
				switch resource.Type {
				case "google_access_context_manager_service_perimeter":
					attributes["id"] = attributes["name"]
				case "google_access_context_manager_service_perimeter_ingress_policy":
					attributes["id"] = attributes["perimeter"]
					attributes["access_policy_id"] = "123456789012"
				default:
					t.Fatalf("unexpected seed resource: %s", resource.Address)
				}
				instance := map[string]any{"schema_version": 0, "attributes": attributes}
				if resource.Index != nil {
					instance["index_key"] = resource.Index
				}
				instances = append(instances, map[string]any{
					"module": "module.perimeter", "mode": "managed", "type": resource.Type,
					"name": resource.Name, "provider": `provider["registry.terraform.io/hashicorp/google"]`,
					"instances": []any{instance},
				})
			}
			// This is local synthetic state. No apply, import, or remote backend.
			writeJSON(t, filepath.Join(dir, "terraform.tfstate"), map[string]any{
				"version": 4, "serial": 1, "lineage": "12345678-1234-1234-1234-123456789012",
				"outputs": map[string]any{}, "resources": instances,
			})
			assertActions(t, readPlan(t, dir, variables), []string{"no-op"})
			variables["identities"] = []string{"serviceAccount:retained@example.iam.gserviceaccount.com"}
			assertActions(t, readPlan(t, dir, variables), []string{"update"})
			variables["rule_title"] = "replacement"
			assertActions(t, readPlan(t, dir, variables), []string{"create", "delete"})
		})
	}
}
