package ui

import "os/exec"

// agentSpec describes one supported coding agent CLI.
type agentSpec struct {
	kind        string
	binary      string   // default binary name on $PATH
	resume      []string // args that resume the latest session
	resumeFirst bool     // resume args are a subcommand and must come first
}

// agentSpecs lists the supported agents in menu/cycle order.
var agentSpecs = []agentSpec{
	{kind: "zot", binary: "zot", resume: []string{"--continue"}},
	{kind: "pi", binary: "pi", resume: []string{"--continue"}},
	{kind: "claude", binary: "claude", resume: []string{"--continue"}},
	{kind: "codex", binary: "codex", resume: []string{"resume", "--last"}, resumeFirst: true},
}

func agentByKind(kind string) *agentSpec {
	for index := range agentSpecs {
		if agentSpecs[index].kind == kind {
			return &agentSpecs[index]
		}
	}
	return nil
}

func isAgentKind(kind string) bool {
	return agentByKind(kind) != nil
}

// binaryFor resolves the launch binary for an agent kind, honoring
// per-agent overrides from the CLI flags.
func (c Config) binaryFor(kind string) string {
	if override, ok := c.AgentBins[kind]; ok && override != "" {
		return override
	}
	if spec := agentByKind(kind); spec != nil {
		return spec.binary
	}
	return kind
}

// availableAgents returns the agent kinds whose binary is on $PATH (or
// overridden), falling back to just the default agent when none are found.
func (m Model) availableAgents() []string {
	var found []string
	for _, spec := range agentSpecs {
		if _, err := exec.LookPath(m.config.binaryFor(spec.kind)); err == nil {
			found = append(found, spec.kind)
		}
	}
	if len(found) == 0 {
		found = []string{m.config.DefaultAgent}
	}
	return found
}

// cycleAgent advances the default agent to the next installed one.
func (m *Model) cycleAgent() {
	available := m.availableAgents()
	for index, kind := range available {
		if kind == m.config.DefaultAgent {
			m.config.DefaultAgent = available[(index+1)%len(available)]
			return
		}
	}
	m.config.DefaultAgent = available[0]
}
