package rules

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/najemi-software/jobsworth/internal/finding"
	"github.com/najemi-software/jobsworth/internal/workflow"
)

var (
	reSecret      = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])secrets\.([A-Za-z0-9_-]+)`)
	reSecretB     = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])secrets\[\s*'([A-Za-z0-9_-]+)'\s*\]`)
	reWholeSecret = regexp.MustCompile(`^\s*secrets\.([A-Za-z0-9_-]+)\s*$`)
)

// SecretRefs returns every secrets.<name> reference in an expression.
func SecretRefs(expr string) []string {
	var out []string
	for _, re := range []*regexp.Regexp{reSecret, reSecretB} {
		for _, m := range re.FindAllStringSubmatch(expr, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// CheckCallSecrets implements medium §4.1 for one reusable-workflow job
// against the callee's on.workflow_call.secrets.
func CheckCallSecrets(job workflow.Job, callee *workflow.File, calleeName string) []finding.Finding {
	if job.Uses == nil || callee == nil || job.SecretsExpr {
		return nil
	}
	label := fmt.Sprintf("job '%s' calling %s", job.Name, calleeName)
	declared := map[string]workflow.Secret{}
	for _, s := range callee.Secrets {
		declared[s.Name] = s
	}
	var out []finding.Finding
	if job.SecretsInherit {
		if len(callee.Secrets) == 0 {
			out = append(out, at("call-secret-inherit-partial", job.SecretsPos, fmt.Sprintf("%s: secrets: inherit on a callee that declares no secrets", label)))
		}
		return out
	}
	supplied := map[string]bool{}
	for _, s := range job.Secrets {
		supplied[s.Name] = true
		if _, ok := declared[s.Name]; !ok {
			out = append(out, at("call-secret-unknown", s.Value.Pos, fmt.Sprintf("%s: secrets.%s is supplied but the callee declares no secret %q", label, s.Name, s.Name)))
		}
		if s.Value.Tag != "" && !s.Value.IsExpr() && strings.TrimSpace(s.Value.Value) != "" {
			out = append(out, at("call-secret-literal", s.Value.Pos, fmt.Sprintf("%s: secrets.%s is a literal value in source; pass ${{ secrets.%s }}", label, s.Name, strings.ToUpper(s.Name))))
		}
	}
	for _, s := range callee.Secrets {
		if s.Required && !supplied[s.Name] {
			out = append(out, at("call-secret-missing", job.Uses.Pos, fmt.Sprintf("%s: required secret %q is not supplied (and secrets: inherit is not set)", label, s.Name)))
		}
	}
	return out
}

// CheckSecrets implements medium §4.2 and §4.3 over one file. inheritCaller
// says whether any known caller passes secrets: inherit.
// inheritCaller: some known caller passes secrets: inherit (warning).
// hasLocalCaller: at least one caller is known; without one the contract
// cannot be verified from here, so the finding is a warning (review 2 N7).
func CheckSecrets(f *workflow.File, inheritCaller, hasLocalCaller bool) []finding.Finding {
	var out []finding.Finding
	declared := map[string]workflow.Secret{}
	for _, s := range f.Secrets {
		declared[s.Name] = s
		if strings.EqualFold(s.Name, "GITHUB_TOKEN") {
			out = append(out, at("secret-github-token-declared", s.Pos, "GITHUB_TOKEN is always available to the callee and need not be declared as a secret"))
		}
	}
	referenced := map[string]bool{}
	hasOtherTrigger := false
	for _, ev := range f.Events {
		if ev != "workflow_call" {
			hasOtherTrigger = true
		}
	}
	for _, e := range f.Exprs {
		refs := SecretRefs(e.Text)
		for _, name := range refs {
			referenced[name] = true
			if f.Kind != workflow.KindWorkflow {
				out = append(out, at("secret-in-action", e.Pos, fmt.Sprintf("secrets.%s is referenced in an action; the secrets context does not exist there, pass it in through inputs", name)))
				continue
			}
			if !f.HasWorkflowCall || hasOtherTrigger || strings.EqualFold(name, "GITHUB_TOKEN") {
				continue
			}
			if _, ok := declared[name]; !ok {
				fd := at("secret-undeclared", e.Pos, fmt.Sprintf("secrets.%s is referenced but not declared under on.workflow_call.secrets", name))
				switch {
				case inheritCaller:
					fd.Severity = finding.Warning
					fd.Message += " (a caller passes secrets: inherit, so it may resolve at runtime)"
				case !hasLocalCaller:
					fd.Severity = finding.Warning
					fd.Message += " (no caller in this repository; the contract cannot be verified)"
				}
				out = append(out, fd)
			}
		}
		if len(refs) > 0 && (e.Key == "name" || e.Key == "run-name") {
			out = append(out, at("secret-in-name", e.Pos, fmt.Sprintf("secrets.%s appears in a %s:, which is logged in clear", refs[0], e.Key)))
		}
		if e.InOutputs && reWholeSecret.MatchString(e.Text) {
			out = append(out, at("secret-to-output", e.Pos, fmt.Sprintf("output value is %s; the runner masks it and consumers see an empty string", strings.TrimSpace(e.Text))))
		}
	}
	for _, s := range f.Secrets {
		if !referenced[s.Name] {
			out = append(out, at("secret-unused", s.Pos, fmt.Sprintf("secret %q is declared but secrets.%s is never referenced", s.Name, s.Name)))
		}
	}
	return out
}
