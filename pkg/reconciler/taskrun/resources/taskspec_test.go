/*
Copyright 2019 The Tekton Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/fake"
	"github.com/tektoncd/pipeline/pkg/reconciler/taskrun/resources"
	"github.com/tektoncd/pipeline/pkg/trustedresources"
	"github.com/tektoncd/pipeline/test/diff"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetTaskSpec_Ref(t *testing.T) {
	task := &v1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orchestrate",
		},
		Spec: v1.TaskSpec{
			Steps: []v1.Step{{
				Name: "step1",
			}},
		},
	}
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskRef: &v1.TaskRef{
				Name: "orchestrate",
			},
		},
	}

	gt := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return task, sampleRefSource.DeepCopy(), nil, nil
	}
	resolvedObjectMeta, taskSpec, err := resources.GetTaskData(context.Background(), tr, gt)

	if err != nil {
		t.Fatalf("Did not expect error getting task spec but got: %s", err)
	}

	if resolvedObjectMeta.Name != "orchestrate" {
		t.Errorf("Expected task name to be `orchestrate` but was %q", resolvedObjectMeta.Name)
	}

	if len(taskSpec.Steps) != 1 || taskSpec.Steps[0].Name != "step1" {
		t.Errorf("Task Spec not resolved as expected, expected referenced Task spec but got: %v", taskSpec)
	}
	if d := cmp.Diff(sampleRefSource, resolvedObjectMeta.RefSource); d != "" {
		t.Errorf("refSource did not match: %s", diff.PrintWantGot(d))
	}
}

func TestGetTaskSpec_Embedded(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskSpec: &v1.TaskSpec{
				Steps: []v1.Step{{
					Name: "step1",
				}},
			},
		},
	}
	gt := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return nil, nil, nil, errors.New("shouldn't be called")
	}
	resolvedObjectMeta, taskSpec, err := resources.GetTaskData(context.Background(), tr, gt)

	if err != nil {
		t.Fatalf("Did not expect error getting task spec but got: %s", err)
	}

	if resolvedObjectMeta.Name != "mytaskrun" {
		t.Errorf("Expected task name for embedded task to default to name of task run but was %q", resolvedObjectMeta.Name)
	}

	if len(taskSpec.Steps) != 1 || taskSpec.Steps[0].Name != "step1" {
		t.Errorf("Task Spec not resolved as expected, expected embedded Task spec but got: %v", taskSpec)
	}

	// embedded tasks have empty RefSource for now. This may be changed in future.
	if resolvedObjectMeta.RefSource != nil {
		t.Errorf("resolved refSource for embedded task is expected to be empty, but got %v", resolvedObjectMeta.RefSource)
	}
}

func TestGetTaskSpec_Invalid(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
	}
	gt := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return nil, nil, nil, errors.New("shouldn't be called")
	}
	_, _, err := resources.GetTaskData(context.Background(), tr, gt)
	if err == nil {
		t.Fatalf("Expected error resolving spec with no embedded or referenced task spec but didn't get error")
	}
}

func TestGetTaskSpec_Error(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskRef: &v1.TaskRef{
				Name: "orchestrate",
			},
		},
	}
	gt := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return nil, nil, nil, errors.New("something went wrong")
	}
	_, _, err := resources.GetTaskData(context.Background(), tr, gt)
	if err == nil {
		t.Fatalf("Expected error when unable to find referenced Task but got none")
	}
}

func TestGetTaskData_ResolutionSuccess(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskRef: &v1.TaskRef{
				ResolverRef: v1.ResolverRef{
					Resolver: "foo",
					Params: []v1.Param{{
						Name: "bar",
						Value: v1.ParamValue{
							Type:      v1.ParamTypeString,
							StringVal: "baz",
						},
					}},
				},
			},
		},
	}
	sourceMeta := metav1.ObjectMeta{
		Name: "task",
	}
	sourceSpec := v1.TaskSpec{
		Steps: []v1.Step{{
			Name:   "step1",
			Image:  "ubuntu",
			Script: `echo "hello world!"`,
		}},
	}

	getTask := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return &v1.Task{
			ObjectMeta: *sourceMeta.DeepCopy(),
			Spec:       *sourceSpec.DeepCopy(),
		}, sampleRefSource.DeepCopy(), nil, nil
	}
	ctx := context.Background()
	resolvedMeta, resolvedSpec, err := resources.GetTaskData(ctx, tr, getTask)
	if err != nil {
		t.Fatalf("Unexpected error getting mocked data: %v", err)
	}
	if sourceMeta.Name != resolvedMeta.Name {
		t.Errorf("Expected name %q but resolved to %q", sourceMeta.Name, resolvedMeta.Name)
	}

	if d := cmp.Diff(sampleRefSource, resolvedMeta.RefSource); d != "" {
		t.Errorf("refSource did not match: %s", diff.PrintWantGot(d))
	}

	if d := cmp.Diff(sourceSpec, *resolvedSpec); d != "" {
		t.Errorf(diff.PrintWantGot(d))
	}
}

func TestGetPipelineData_ResolutionError(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskRef: &v1.TaskRef{
				ResolverRef: v1.ResolverRef{
					Resolver: "git",
				},
			},
		},
	}
	getTask := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return nil, nil, nil, errors.New("something went wrong")
	}
	ctx := context.Background()
	_, _, err := resources.GetTaskData(ctx, tr, getTask)
	if err == nil {
		t.Fatalf("Expected error when unable to find referenced Task but got none")
	}
}

func TestGetTaskData_ResolvedNilTask(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskRef: &v1.TaskRef{
				ResolverRef: v1.ResolverRef{
					Resolver: "git",
				},
			},
		},
	}
	getTask := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return nil, nil, nil, nil
	}
	ctx := context.Background()
	_, _, err := resources.GetTaskData(ctx, tr, getTask)
	if err == nil {
		t.Fatalf("Expected error when unable to find referenced Task but got none")
	}
}

func TestGetTaskData_VerificationResult(t *testing.T) {
	tr := &v1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mytaskrun",
		},
		Spec: v1.TaskRunSpec{
			TaskRef: &v1.TaskRef{
				ResolverRef: v1.ResolverRef{
					Resolver: "foo",
					Params: v1.Params{{
						Name: "bar",
						Value: v1.ParamValue{
							Type:      v1.ParamTypeString,
							StringVal: "baz",
						},
					}},
				},
			},
		},
	}
	sourceMeta := metav1.ObjectMeta{
		Name: "task",
	}
	sourceSpec := v1.TaskSpec{
		Steps: []v1.Step{{
			Name:   "step1",
			Image:  "ubuntu",
			Script: `echo "hello world!"`,
		}},
	}

	verificationResult := &trustedresources.VerificationResult{
		VerificationResultType: trustedresources.VerificationError,
		Err:                    trustedresources.ErrResourceVerificationFailed,
	}
	getTask := func(ctx context.Context, n string) (*v1.Task, *v1.RefSource, *trustedresources.VerificationResult, error) {
		return &v1.Task{
			ObjectMeta: *sourceMeta.DeepCopy(),
			Spec:       *sourceSpec.DeepCopy(),
		}, nil, verificationResult, nil
	}
	r, _, err := resources.GetTaskData(context.Background(), tr, getTask)
	if err != nil {
		t.Fatalf("Did not expect error but got: %s", err)
	}
	if d := cmp.Diff(verificationResult, r.VerificationResult, cmpopts.EquateErrors()); d != "" {
		t.Errorf(diff.PrintWantGot(d))
	}
}

func TestGetStepActionsData(t *testing.T) {
	taskRunUser := int64(1001)
	stepActionUser := int64(1000)
	tests := []struct {
		name       string
		tr         *v1.TaskRun
		stepAction *v1alpha1.StepAction
		want       []v1.Step
	}{{
		name: "step-action-with-command-args",
		tr: &v1.TaskRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mytaskrun",
				Namespace: "default",
			},
			Spec: v1.TaskRunSpec{
				TaskSpec: &v1.TaskSpec{
					Steps: []v1.Step{{
						Ref: &v1.Ref{
							Name: "stepAction",
						},
						WorkingDir: "/bar",
						Timeout:    &metav1.Duration{Duration: time.Hour},
					}},
				},
			},
		},
		stepAction: &v1alpha1.StepAction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stepAction",
				Namespace: "default",
			},
			Spec: v1alpha1.StepActionSpec{
				Image:   "myimage",
				Command: []string{"ls"},
				Args:    []string{"-lh"},
			},
		},
		want: []v1.Step{{
			Image:      "myimage",
			Command:    []string{"ls"},
			Args:       []string{"-lh"},
			WorkingDir: "/bar",
			Timeout:    &metav1.Duration{Duration: time.Hour},
		}},
	}, {
		name: "step-action-with-script",
		tr: &v1.TaskRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mytaskrun",
				Namespace: "default",
			},
			Spec: v1.TaskRunSpec{
				TaskSpec: &v1.TaskSpec{
					Steps: []v1.Step{{
						Ref: &v1.Ref{
							Name: "stepActionWithScript",
						},
					}},
				},
			},
		},
		stepAction: &v1alpha1.StepAction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stepActionWithScript",
				Namespace: "default",
			},
			Spec: v1alpha1.StepActionSpec{
				Image:  "myimage",
				Script: "ls",
			},
		},
		want: []v1.Step{{
			Image:  "myimage",
			Script: "ls",
		}},
	}, {
		name: "step-action-with-env",
		tr: &v1.TaskRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mytaskrun",
				Namespace: "default",
			},
			Spec: v1.TaskRunSpec{
				TaskSpec: &v1.TaskSpec{
					Steps: []v1.Step{{
						Ref: &v1.Ref{
							Name: "stepActionWithEnv",
						},
					}},
				},
			},
		},
		stepAction: &v1alpha1.StepAction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stepActionWithEnv",
				Namespace: "default",
			},
			Spec: v1alpha1.StepActionSpec{
				Image: "myimage",
				Env: []corev1.EnvVar{{
					Name:  "env1",
					Value: "value1",
				}},
			},
		},
		want: []v1.Step{{
			Image: "myimage",
			Env: []corev1.EnvVar{{
				Name:  "env1",
				Value: "value1",
			}},
		}},
	}, {
		name: "inline and ref StepAction",
		tr: &v1.TaskRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mytaskrun",
				Namespace: "default",
			},
			Spec: v1.TaskRunSpec{
				TaskSpec: &v1.TaskSpec{
					Steps: []v1.Step{{
						Ref: &v1.Ref{
							Name: "stepAction",
						},
						WorkingDir: "/bar",
						Timeout:    &metav1.Duration{Duration: time.Hour},
					}, {
						Image:   "foo",
						Command: []string{"ls"},
					}},
				},
			},
		},
		stepAction: &v1alpha1.StepAction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stepAction",
				Namespace: "default",
			},
			Spec: v1alpha1.StepActionSpec{
				Image:   "myimage",
				Command: []string{"ls"},
				Args:    []string{"-lh"},
			},
		},
		want: []v1.Step{{
			Image:      "myimage",
			Command:    []string{"ls"},
			Args:       []string{"-lh"},
			WorkingDir: "/bar",
			Timeout:    &metav1.Duration{Duration: time.Hour},
		}, {
			Image:   "foo",
			Command: []string{"ls"},
		}},
	}, {
		name: "step-action-with-security-context-overwritten",
		tr: &v1.TaskRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mytaskrun",
				Namespace: "default",
			},
			Spec: v1.TaskRunSpec{
				TaskSpec: &v1.TaskSpec{
					Steps: []v1.Step{{
						Ref: &v1.Ref{
							Name: "stepAction",
						},
						SecurityContext: &corev1.SecurityContext{RunAsUser: &taskRunUser},
					}},
				},
			},
		},
		stepAction: &v1alpha1.StepAction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stepAction",
				Namespace: "default",
			},
			Spec: v1alpha1.StepActionSpec{
				Image:           "myimage",
				Command:         []string{"ls"},
				Args:            []string{"-lh"},
				SecurityContext: &corev1.SecurityContext{RunAsUser: &stepActionUser},
			},
		},
		want: []v1.Step{{
			Image:           "myimage",
			Command:         []string{"ls"},
			Args:            []string{"-lh"},
			SecurityContext: &corev1.SecurityContext{RunAsUser: &stepActionUser},
		}},
	}}
	for _, tt := range tests {
		ctx := context.Background()
		tektonclient := fake.NewSimpleClientset(tt.stepAction)

		got, err := resources.GetStepActionsData(ctx, *tt.tr.Spec.TaskSpec, tt.tr, tektonclient, nil, nil)
		if err != nil {
			t.Errorf("Did not expect an error but got : %s", err)
		}
		if d := cmp.Diff(tt.want, got); d != "" {
			t.Errorf("the taskSpec did not match what was expected diff: %s", diff.PrintWantGot(d))
		}
	}
}

func TestGetStepActionsData_Error(t *testing.T) {
	tests := []struct {
		name           string
		tr             *v1.TaskRun
		stepActionFunc func(ctx context.Context, ref *v1.Ref) (*v1alpha1.StepAction, *v1.RefSource, error)
		expectedError  error
	}{{
		name: "namespace missing error",
		tr: &v1.TaskRun{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mytaskrun",
			},
			Spec: v1.TaskRunSpec{
				TaskSpec: &v1.TaskSpec{
					Steps: []v1.Step{{
						Ref: &v1.Ref{
							Name: "stepActionError",
						},
					}},
				},
			},
		},
		expectedError: fmt.Errorf("must specify namespace to resolve reference to step action stepActionError"),
	}}
	for _, tt := range tests {
		_, err := resources.GetStepActionsData(context.Background(), *tt.tr.Spec.TaskSpec, tt.tr, nil, nil, nil)
		if err == nil {
			t.Fatalf("Expected to get an error but did not find any.")
		}
		if d := cmp.Diff(tt.expectedError.Error(), err.Error()); d != "" {
			t.Errorf("the expected error did not match what was received: %s", diff.PrintWantGot(d))
		}
	}
}
