package workflow

import "testing"

func TestProbeKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want Kind
		ok   bool
	}{
		{"workflow", "on: push\njobs:\n  a:\n    runs-on: x\n", KindWorkflow, true},
		{"composite", "runs:\n  using: composite\n  steps: []\n", KindCompositeAction, true},
		{"node", "runs:\n  using: node20\n  main: index.js\n", KindOtherAction, true},
		{"docker", "runs:\n  using: docker\n  image: Dockerfile\n", KindOtherAction, true},
		{"runs not mapping", "runs: composite\n", KindUnknown, true},
		{"runs without using", "runs:\n  steps: []\n", KindUnknown, true},
		{"config yaml", "repos:\n  a: b\n", KindUnknown, true},
		{"empty", "", KindUnknown, false},
		{"sequence root", "- a\n- b\n", KindUnknown, false},
		{"scalar root", "hello\n", KindUnknown, false},
		{"malformed", "jobs: [\n", KindUnknown, false},
		{"jobs wins over runs", "jobs: {}\nruns:\n  using: composite\n", KindWorkflow, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ProbeKind([]byte(tc.in))
			if got != tc.want || ok != tc.ok {
				t.Fatalf("ProbeKind = %v,%v want %v,%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
