/*
Copyright 2018 The Kubernetes Authors.

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

package main

import (
	"flag"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/test-infra/prow/flagutil"
	configflagutil "k8s.io/test-infra/prow/flagutil/config"
	"k8s.io/test-infra/prow/gerrit/client"
)

func TestFlags(t *testing.T) {
	cases := []struct {
		name     string
		args     map[string]string
		del      sets.String
		expected func(*options)
		err      bool
	}{
		{
			name: "minimal flags work",
		},
		{
			name: "expicitly set --dry-run=false",
			args: map[string]string{
				"--dry-run": "false",
			},
			expected: func(o *options) {
				o.dryRun = false
			},
		},
		{
			name: "explicitly set --dry-run=true",
			args: map[string]string{
				"--dry-run": "true",
			},
			expected: func(o *options) {
				o.dryRun = true
			},
		},
		{
			name:     "dry run defaults to false",
			del:      sets.NewString("--dry-run"),
			expected: func(o *options) {},
		},
		{
			name: "gcs credentials are set",
			args: map[string]string{
				"--gcs-credentials-file": "/creds",
			},
			expected: func(o *options) {
				o.storage.GCSCredentialsFile = "/creds"
			},
		},
		{
			name: "s3 credentials are set",
			args: map[string]string{
				"--s3-credentials-file": "/creds",
			},
			expected: func(o *options) {
				o.storage.S3CredentialsFile = "/creds"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expected := &options{
				projects:           client.ProjectsFlag{},
				projectsOptOutHelp: client.ProjectsFlag{},
				lastSyncFallback:   "gs://path",
				config: configflagutil.ConfigOptions{
					ConfigPathFlagName:                    "config-path",
					JobConfigPathFlagName:                 "job-config-path",
					ConfigPath:                            "yo",
					SupplementalProwConfigsFileNameSuffix: "_prowconfig.yaml",
				},
				dryRun:                  false,
				instrumentationOptions:  flagutil.DefaultInstrumentationOptions(),
				inRepoConfigCacheSize:   100,
				inRepoConfigCacheCopies: 1,
				changeWorkerPoolSize:    1,
			}
			expected.projects.Set("foo=bar,baz")
			expected.projectsOptOutHelp.Set("foo=bar")
			if tc.expected != nil {
				tc.expected(expected)
			}
			argMap := map[string]string{
				"--gerrit-projects":              "foo=bar,baz",
				"--gerrit-projects-opt-out-help": "foo=bar",
				"--last-sync-fallback":           "gs://path",
				"--config-path":                  "yo",
				"--dry-run":                      "false",
			}
			for k, v := range tc.args {
				argMap[k] = v
			}
			for k := range tc.del {
				delete(argMap, k)
			}

			var args []string
			for k, v := range argMap {
				args = append(args, k+"="+v)
			}
			fs := flag.NewFlagSet("fake-flags", flag.PanicOnError)
			actual := gatherOptions(fs, args...)
			switch err := actual.validate(); {
			case err != nil:
				if !tc.err {
					t.Errorf("unexpected error: %v", err)
				}
			case tc.err:
				t.Errorf("failed to receive expected error")
			case !reflect.DeepEqual(*expected, actual):
				t.Errorf("%#v != expected %#v", actual, *expected)
			}
		})
	}
}
