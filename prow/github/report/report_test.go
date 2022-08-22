/*
Copyright 2017 The Kubernetes Authors.

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

package report

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	prowapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/kube"
)

const fakeBotName = "k8s-bot"

func TestParseIssueComment(t *testing.T) {
	var testcases = []struct {
		name            string
		context         string
		state           string
		ics             []github.IssueComment
		expectedDeletes []int
		expectedEntries []string
		expectedUpdate  int
		isOptional      bool
	}{
		{
			name:            "should create a new comment",
			context:         "bla test",
			state:           github.StatusFailure,
			expectedEntries: []string{createReportEntry("bla test", true)},
		},
		{
			name:            "should create a new optional comment",
			context:         "bla test",
			state:           github.StatusFailure,
			isOptional:      true,
			expectedEntries: []string{createReportEntry("bla test", false)},
		},
		{
			name:    "should not delete an up-to-date comment",
			context: "bla test",
			state:   github.StatusSuccess,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nfoo test | something | or other\n\n",
				},
			},
		},
		{
			name:    "should delete when all tests pass",
			context: "bla test",
			state:   github.StatusSuccess,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nbla test | something | or other\n\n" + commentTag,
					ID:   123,
				},
			},
			expectedDeletes: []int{123},
			expectedEntries: []string{},
		},
		{
			name:    "should delete a passing test with \\r",
			context: "bla test",
			state:   github.StatusSuccess,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\r\nbla test | something | or other\r\n\r\n" + commentTag,
					ID:   123,
				},
			},
			expectedDeletes: []int{123},
			expectedEntries: []string{},
		},

		{
			name:    "should update a failed test",
			context: "bla test",
			state:   github.StatusFailure,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nbla test | something | or other\n\n" + commentTag,
					ID:   123,
				},
			},
			expectedDeletes: []int{123},
			expectedEntries: []string{"bla test"},
		},
		{
			name:    "should preserve old results when updating",
			context: "bla test",
			state:   github.StatusFailure,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nbla test | something | or other\nfoo test | wow | aye\n\n" + commentTag,
					ID:   123,
				},
			},
			expectedDeletes: []int{123},
			expectedEntries: []string{"bla test", "foo test"},
		},
		{
			name:    "should merge duplicates",
			context: "bla test",
			state:   github.StatusFailure,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nbla test | something | or other\nfoo test | wow such\n\n" + commentTag,
					ID:   123,
				},
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nfoo test | beep | boop\n\n" + commentTag,
					ID:   124,
				},
			},
			expectedDeletes: []int{123, 124},
			expectedEntries: []string{"bla test", "foo test"},
		},
		{
			name:    "should update an old comment when a test passes",
			context: "bla test",
			state:   github.StatusSuccess,
			ics: []github.IssueComment{
				{
					User: github.User{Login: "k8s-ci-robot"},
					Body: "--- | --- | ---\nbla test | something | or other\nfoo test | wow | aye\n\n" + commentTag,
					ID:   123,
				},
			},
			expectedDeletes: []int{},
			expectedEntries: []string{"foo test"},
			expectedUpdate:  123,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pj := prowapi.ProwJob{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						kube.IsOptionalLabel: strconv.FormatBool(tc.isOptional),
					},
				},
				Spec: prowapi.ProwJobSpec{
					Type:    prowapi.PresubmitJob,
					Context: tc.context,
					Refs:    &prowapi.Refs{Pulls: []prowapi.Pull{{}}},
				},
				Status: prowapi.ProwJobStatus{
					State: prowapi.ProwJobState(tc.state),
				},
			}
			isBot := func(candidate string) bool {
				return candidate == "k8s-ci-robot"
			}
			deletes, entries, update := parseIssueComments([]prowapi.ProwJob{pj}, isBot, tc.ics)
			if len(deletes) != len(tc.expectedDeletes) {
				t.Errorf("It %q: wrong number of deletes. Got %v, expected %v", tc.name, deletes, tc.expectedDeletes)
			} else {
				for _, edel := range tc.expectedDeletes {
					found := false
					for _, del := range deletes {
						if del == edel {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("It %q: expected to find %d in %v", tc.name, edel, deletes)
					}
				}
			}
			if len(entries) != len(tc.expectedEntries) {
				t.Errorf("It %q: wrong number of entries. Got %v, expected %v", tc.name, entries, tc.expectedEntries)
			}
			if tc.expectedUpdate != update {
				t.Errorf("It %q: expected update %d, got %d", tc.name, tc.expectedUpdate, update)
			}

			for _, expectedEntry := range tc.expectedEntries {
				found := false
				for _, ent := range entries {
					if strings.Contains(ent, expectedEntry) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("It %q: expected to find %q in %v", tc.name, expectedEntry, entries)
				}
			}
		})
	}
}

func createReportEntry(context string, isRequired bool) string {
	return fmt.Sprintf("%s |  | [link]() | %s | ", context, strconv.FormatBool(isRequired))
}

type fakeGhClient struct {
	status         []github.Status
	commitComments map[string][]github.IssueComment
	issueComments  map[string][]github.IssueComment
}

func (gh fakeGhClient) BotUserCheckerWithContext(_ context.Context) (func(string) bool, error) {
	return func(candidate string) bool {
		return candidate == fakeBotName
	}, nil
}

const maxLen = 140

func (gh *fakeGhClient) CreateStatusWithContext(_ context.Context, org, repo, ref string, s github.Status) error {
	if d := s.Description; len(d) > maxLen {
		return fmt.Errorf("%s is len %d, more than max of %d chars", d, len(d), maxLen)
	}
	gh.status = append(gh.status, s)
	return nil

}
func (gh fakeGhClient) ListIssueCommentsWithContext(_ context.Context, org, repo string, number int) ([]github.IssueComment, error) {
	var comments []github.IssueComment
	for _, c := range gh.issueComments[fmt.Sprintf("%s/%s/%d", org, repo, number)] {
		comments = append(comments, c)
	}
	return comments, nil
}

func (gh *fakeGhClient) CreateCommentWithContext(_ context.Context, org, repo string, number int, comment string) error {
	if gh.issueComments == nil {
		gh.issueComments = make(map[string][]github.IssueComment)
	}
	key := fmt.Sprintf("%s/%s/%d", org, repo, number)
	gh.issueComments[key] = append(gh.issueComments[key],
		github.IssueComment{
			ID:   len(gh.issueComments[key]),
			Body: comment,
		},
	)
	return nil
}
func (gh fakeGhClient) DeleteCommentWithContext(_ context.Context, org, repo string, ID int) error {
	return nil
}
func (gh fakeGhClient) EditCommentWithContext(_ context.Context, org, repo string, ID int, comment string) error {
	return nil
}
func (gh fakeGhClient) ListCommitCommentsWithContext(ctx context.Context, org, repo, SHA string) ([]github.IssueComment, error) {
	var comments []github.IssueComment
	key := fmt.Sprintf("%s/%s/%s", org, repo, SHA)
	for _, c := range gh.commitComments[key] {
		comments = append(comments, c)
	}
	return comments, nil
}
func (gh *fakeGhClient) CreateCommitCommentWithContext(ctx context.Context, org, repo, SHA, comment string) error {
	if gh.commitComments == nil {
		gh.commitComments = make(map[string][]github.IssueComment)
	}
	key := fmt.Sprintf("%s/%s/%s", org, repo, SHA)
	gh.commitComments[key] = append(gh.commitComments[key], github.IssueComment{
		ID:   len(gh.commitComments[key]),
		Body: comment,
	})
	return nil
}

func shout(i int) string {
	if i == 0 {
		return "start"
	}
	return fmt.Sprintf("%s part%d", shout(i-1), i)
}

func TestReportStatus(t *testing.T) {
	const (
		defMsg = "default-message"
	)
	tests := []struct {
		name string

		state            prowapi.ProwJobState
		report           bool
		desc             string // override default msg
		pjType           prowapi.ProwJobType
		expectedStatuses []string
		expectedDesc     string
	}{
		{
			name: "Successful prowjob with report true should set status",

			state:            prowapi.SuccessState,
			pjType:           prowapi.PresubmitJob,
			report:           true,
			expectedStatuses: []string{"success"},
		},
		{
			name: "Successful prowjob with report false should not set status",

			state:            prowapi.SuccessState,
			pjType:           prowapi.PresubmitJob,
			report:           false,
			expectedStatuses: []string{},
		},
		{
			name: "Pending prowjob with report true should set status",

			state:            prowapi.PendingState,
			report:           true,
			pjType:           prowapi.PresubmitJob,
			expectedStatuses: []string{"pending"},
		},
		{
			name: "Aborted presubmit job with report true should set failure status",

			state:            prowapi.AbortedState,
			report:           true,
			pjType:           prowapi.PresubmitJob,
			expectedStatuses: []string{"failure"},
		},
		{
			name: "Triggered presubmit job with report true should set pending status",

			state:            prowapi.TriggeredState,
			report:           true,
			pjType:           prowapi.PresubmitJob,
			expectedStatuses: []string{"pending"},
		},
		{
			name: "really long description is truncated",

			state:            prowapi.TriggeredState,
			report:           true,
			expectedStatuses: []string{"pending"},
			desc:             shout(maxLen), // resulting string will exceed maxLen
			expectedDesc:     config.ContextDescriptionWithBaseSha(shout(maxLen), ""),
		},
		{
			name: "Successful postsubmit job with report true should set success status",

			state:  prowapi.SuccessState,
			report: true,
			pjType: prowapi.PostsubmitJob,

			expectedStatuses: []string{"success"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			ghc := &fakeGhClient{}

			if tc.desc == "" {
				tc.desc = defMsg
			}
			if tc.expectedDesc == "" {
				tc.expectedDesc = defMsg
			}
			pj := prowapi.ProwJob{
				Status: prowapi.ProwJobStatus{
					State:       tc.state,
					Description: tc.desc,
					URL:         "http://mytest.com",
				},
				Spec: prowapi.ProwJobSpec{
					Job:     "job-name",
					Type:    tc.pjType,
					Context: "parent",
					Report:  tc.report,
					Refs: &prowapi.Refs{
						Org:  "k8s",
						Repo: "test-infra",
						Pulls: []prowapi.Pull{{
							Author: "me",
							Number: 1,
							SHA:    "abcdef",
						}},
					},
				},
			}
			// Run
			if err := reportStatus(context.Background(), ghc, pj); err != nil {
				t.Error(err)
			}
			// Check
			if len(ghc.status) != len(tc.expectedStatuses) {
				t.Errorf("expected %d status(es), found %d", len(tc.expectedStatuses), len(ghc.status))
				return
			}
			for i, status := range ghc.status {
				if status.State != tc.expectedStatuses[i] {
					t.Errorf("unexpected status: %s, expected: %s", status.State, tc.expectedStatuses[i])
				}
				if i == 0 && status.Description != tc.expectedDesc {
					t.Errorf("description %d %s != expected %s", i, status.Description, tc.expectedDesc)
				}
			}
		})
	}
}

func TestShouldReport(t *testing.T) {
	var testcases = []struct {
		name       string
		pj         prowapi.ProwJob
		validTypes []prowapi.ProwJobType
		report     bool
	}{
		{
			name: "should not report skip report job",
			pj: prowapi.ProwJob{
				Spec: prowapi.ProwJobSpec{
					Type:   prowapi.PresubmitJob,
					Report: false,
				},
			},
			validTypes: []prowapi.ProwJobType{prowapi.PresubmitJob},
		},
		{
			name: "should report presubmit job",
			pj: prowapi.ProwJob{
				Spec: prowapi.ProwJobSpec{
					Type:   prowapi.PresubmitJob,
					Report: true,
				},
			},
			validTypes: []prowapi.ProwJobType{prowapi.PresubmitJob},
			report:     true,
		},
		{
			name: "should not report postsubmit job",
			pj: prowapi.ProwJob{
				Spec: prowapi.ProwJobSpec{
					Type:   prowapi.PostsubmitJob,
					Report: true,
				},
			},
			validTypes: []prowapi.ProwJobType{prowapi.PresubmitJob},
		},
		{
			name: "should report postsubmit job if told to",
			pj: prowapi.ProwJob{
				Spec: prowapi.ProwJobSpec{
					Type:   prowapi.PostsubmitJob,
					Report: true,
				},
			},
			validTypes: []prowapi.ProwJobType{prowapi.PresubmitJob, prowapi.PostsubmitJob},
			report:     true,
		},
	}

	for _, tc := range testcases {
		r := ShouldReport(tc.pj, tc.validTypes)

		if r != tc.report {
			t.Errorf("Unexpected result from test: %s.\nExpected: %v\nGot: %v",
				tc.name, tc.report, r)
		}
	}
}

func TestCreateComment(t *testing.T) {
	tests := []struct {
		name     string
		template *template.Template
		pjs      []prowapi.ProwJob
		entries  []string
		want     string
		wantErr  bool
	}{
		{
			name:     "single-job-single-failure",
			template: mustParseTemplate(t, ""),
			pjs: []prowapi.ProwJob{
				{
					Spec: prowapi.ProwJobSpec{
						Refs: &prowapi.Refs{
							Pulls: []prowapi.Pull{
								{
									Author: "chaodaig",
								},
							},
						},
					},
				},
			},
			entries: []string{
				"aaa | bbb | ccc | ddd | eee",
			},
			want: `@chaodaig: The following test **failed**, say ` + "`/retest`" + ` to rerun all failed tests or ` + "`/retest-required`" + ` to rerun all mandatory failed tests:

Test name | Commit | Details | Required | Rerun command
--- | --- | --- | --- | ---
aaa | bbb | ccc | ddd | eee



<details>

Instructions for interacting with me using PR comments are available [here](https://git.k8s.io/community/contributors/guide/pull-requests.md).  If you have questions or suggestions related to my behavior, please file an issue against the [kubernetes/test-infra](https://github.com/kubernetes/test-infra/issues/new?title=Prow%20issue:) repository. I understand the commands that are listed [here](https://go.k8s.io/bot-commands).
</details>
<!-- test report -->`,
		},
		{
			name:     "single-job-multi;e-failure",
			template: mustParseTemplate(t, ""),
			pjs: []prowapi.ProwJob{
				{
					Spec: prowapi.ProwJobSpec{
						Refs: &prowapi.Refs{
							Pulls: []prowapi.Pull{
								{
									Author: "chaodaig",
								},
							},
						},
					},
				},
			},
			entries: []string{
				"aaa | bbb | ccc | ddd | eee",
				"fff | ggg | hhh | iii | jjj",
			},
			want: `@chaodaig: The following tests **failed**, say ` + "`/retest`" + ` to rerun all failed tests or ` + "`/retest-required`" + ` to rerun all mandatory failed tests:

Test name | Commit | Details | Required | Rerun command
--- | --- | --- | --- | ---
aaa | bbb | ccc | ddd | eee
fff | ggg | hhh | iii | jjj



<details>

Instructions for interacting with me using PR comments are available [here](https://git.k8s.io/community/contributors/guide/pull-requests.md).  If you have questions or suggestions related to my behavior, please file an issue against the [kubernetes/test-infra](https://github.com/kubernetes/test-infra/issues/new?title=Prow%20issue:) repository. I understand the commands that are listed [here](https://go.k8s.io/bot-commands).
</details>
<!-- test report -->`,
		},
		{
			name:     "multiple-job-only-use-first-one",
			template: mustParseTemplate(t, "{{.Spec.Job}}"),
			pjs: []prowapi.ProwJob{
				{
					Spec: prowapi.ProwJobSpec{
						Job: "job-a",
						Refs: &prowapi.Refs{
							Pulls: []prowapi.Pull{
								{
									Author: "chaodaig",
								},
							},
						},
					},
				},
				{
					Spec: prowapi.ProwJobSpec{
						Job: "job-b",
						Refs: &prowapi.Refs{
							Pulls: []prowapi.Pull{
								{
									Author: "chaodaig",
								},
							},
						},
					},
				},
			},
			entries: []string{
				"aaa | bbb | ccc | ddd | eee",
			},
			want: `@chaodaig: The following test **failed**, say ` + "`/retest`" + ` to rerun all failed tests or ` + "`/retest-required`" + ` to rerun all mandatory failed tests:

Test name | Commit | Details | Required | Rerun command
--- | --- | --- | --- | ---
aaa | bbb | ccc | ddd | eee

job-a

<details>

Instructions for interacting with me using PR comments are available [here](https://git.k8s.io/community/contributors/guide/pull-requests.md).  If you have questions or suggestions related to my behavior, please file an issue against the [kubernetes/test-infra](https://github.com/kubernetes/test-infra/issues/new?title=Prow%20issue:) repository. I understand the commands that are listed [here](https://go.k8s.io/bot-commands).
</details>
<!-- test report -->`,
		},
		{
			name: "multiple-job-all-passed",
			pjs: []prowapi.ProwJob{
				{
					Spec: prowapi.ProwJobSpec{
						Job: "job-a",
						Refs: &prowapi.Refs{
							Pulls: []prowapi.Pull{
								{
									Author: "chaodaig",
								},
							},
						},
					},
				},
				{
					Spec: prowapi.ProwJobSpec{
						Job: "job-b",
						Refs: &prowapi.Refs{
							Pulls: []prowapi.Pull{
								{
									Author: "chaodaig",
								},
							},
						},
					},
				},
			},
			entries: []string{},
			want: `@chaodaig: all tests **passed!**


<details>

Instructions for interacting with me using PR comments are available [here](https://git.k8s.io/community/contributors/guide/pull-requests.md).  If you have questions or suggestions related to my behavior, please file an issue against the [kubernetes/test-infra](https://github.com/kubernetes/test-infra/issues/new?title=Prow%20issue:) repository. I understand the commands that are listed [here](https://go.k8s.io/bot-commands).
</details>
<!-- test report -->`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotComment, gotErr := createComment(tc.template, tc.pjs, tc.entries)
			if diff := cmp.Diff(gotComment, tc.want); diff != "" {
				t.Fatalf("comment mismatch:\n%s", diff)
			}
			if (gotErr != nil && !tc.wantErr) || (gotErr == nil && tc.wantErr) {
				t.Fatalf("error mismatch. got: %v, want: %v", gotErr, tc.wantErr)
			}
		})
	}
}

func mustParseTemplate(t *testing.T, s string) *template.Template {
	tmpl, err := template.New("test").Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func TestReport(t *testing.T) {
	ghCommenter := &prowapi.ReporterConfig{
		GitHub: &prowapi.GitHubReporterConfig{
			CommentOnPostsubmits: true,
		},
	}

	refs := &prowapi.Refs{
		Org:     "k8s",
		Repo:    "test-infra",
		BaseSHA: "SHA",
		Pulls: []prowapi.Pull{{
			Author: "me",
			Number: 1,
			SHA:    "abcdef",
		}},
	}

	now := metav1.Now()
	failedStatus := prowapi.ProwJobStatus{
		State:          prowapi.FailureState,
		URL:            "http://mytest.com",
		CompletionTime: &now,
	}
	successStatus := prowapi.ProwJobStatus{
		State:          prowapi.SuccessState,
		URL:            "http://mytest.com",
		CompletionTime: &now,
	}

	testCases := []struct {
		name               string
		pj                 prowapi.ProwJob
		issueComments      map[string][]github.IssueComment
		expectedComments   int
		expectedPrComments []string
	}{
		{
			name: "postsubmit comments are made on the commit",
			pj: prowapi.ProwJob{
				Status: failedStatus,
				Spec: prowapi.ProwJobSpec{
					ReporterConfig: ghCommenter,
				},
			},
			expectedComments: 1,
			expectedPrComments: []string{
				prCommitNote,
			},
		},
		{
			name: "no comments on commits if comment_on_postsubmits not set",
			pj: prowapi.ProwJob{
				Status: failedStatus,
				Spec:   prowapi.ProwJobSpec{},
			},
		},
		{
			name: "no comments on successfully completed postsubmit",
			pj: prowapi.ProwJob{
				Status: successStatus,
				Spec: prowapi.ProwJobSpec{
					ReporterConfig: ghCommenter,
				},
			},
			expectedPrComments: []string{
				prCommitNote,
			},
		},
		{
			name: "drop a note on PR",
			pj: prowapi.ProwJob{
				Status: failedStatus,
				Spec: prowapi.ProwJobSpec{
					ReporterConfig: ghCommenter,
				},
			},
			expectedComments: 1,
			expectedPrComments: []string{
				prCommitNote,
			},
		},
		{
			name: "comment only once on the PR",
			pj: prowapi.ProwJob{
				Status: successStatus,
				Spec: prowapi.ProwJobSpec{
					ReporterConfig: ghCommenter,
				},
			},
			issueComments: map[string][]github.IssueComment{
				"org/repo/1": {
					{
						Body: prCommitNote,
						User: github.User{Login: fakeBotName},
					},
				},
			},
			expectedPrComments: []string{
				prCommitNote,
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ghc := &fakeGhClient{
				issueComments: tc.issueComments,
			}

			tc.pj.Spec.Job = "postsubmit-job"
			tc.pj.Spec.Type = prowapi.PostsubmitJob
			tc.pj.Spec.Context = "parent"
			tc.pj.Spec.Refs = refs
			tc.pj.Spec.Report = true

			err := Report(context.Background(), ghc, nil, tc.pj, config.GitHubReporter{
				JobTypesToReport: []prowapi.ProwJobType{prowapi.PostsubmitJob},
			})
			if err != nil {
				t.Fatalf("Unexpected error from test: %s: : %v", tc.name, err)
			}
			comments, err := ghc.ListCommitCommentsWithContext(context.Background(), refs.Org, refs.Repo, refs.BaseSHA)
			if err != nil {
				t.Fatalf("Unexpected error from test: %s: %v", tc.name, err)
			}
			if len(comments) != tc.expectedComments {
				t.Fatalf("Expected %d comments, got: %d", tc.expectedComments, len(comments))
			}
			if len(tc.expectedPrComments) == 0 {
				return
			}

			issueComments, _ := ghc.ListIssueCommentsWithContext(context.Background(), refs.Org, refs.Repo, refs.Pulls[0].Number)
			if len(tc.expectedPrComments) != len(issueComments) {
				t.Fatalf("Expected %d issue comments, got: %d", len(tc.expectedPrComments), len(issueComments))
			}
			for i, c := range tc.expectedPrComments {
				if !strings.Contains(issueComments[i].Body, c) {
					t.Fatalf("Expected issue comment to contain %q, got: %q", c, issueComments[i].Body)
				}
			}
		})
	}
}
