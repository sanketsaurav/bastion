package engine

import (
	"strconv"
	"strings"
)

// ApplyResult is what actually happened during an apply run.
type ApplyResult struct {
	Completed    []string            `json:"completed"`
	Failed       string              `json:"failed,omitempty"`
	FailedLogs   []string            `json:"failedLogs,omitempty"`
	Done         bool                `json:"done"`
	LockHeld     bool                `json:"lockHeld,omitempty"`
	LockAgeSecs  int                 `json:"lockAgeSeconds,omitempty"`
	LockTakeover bool                `json:"lockTakeover,omitempty"`
	Logs         map[string][]string `json:"-"`
}

// ApplyHooks stream progress to the caller as events arrive.
type ApplyHooks struct {
	OnStart func(actionID string)
	OnDone  func(actionID, status string)
	OnLog   func(actionID, line string)
}

// eventParser consumes the apply script's "@e"/"@l" line protocol. Steps are
// identified positionally (a0, a1, …) so action IDs containing arbitrary
// characters never touch the protocol.
type eventParser struct {
	plan   *Plan
	hooks  ApplyHooks
	result ApplyResult
}

func newEventParser(plan *Plan, hooks ApplyHooks) *eventParser {
	return &eventParser{plan: plan, hooks: hooks, result: ApplyResult{Logs: map[string][]string{}}}
}

func (p *eventParser) actionID(step string) (string, bool) {
	if !strings.HasPrefix(step, "a") {
		return step, false
	}
	i, err := strconv.Atoi(step[1:])
	if err != nil || i < 0 || i >= len(p.plan.Actions) {
		return step, false
	}
	return p.plan.Actions[i].ID, true
}

const maxKeptLogLines = 40

func (p *eventParser) line(raw string) {
	switch {
	case strings.HasPrefix(raw, "@e "):
		parts := strings.Fields(raw[3:])
		if len(parts) < 2 {
			return
		}
		step, status := parts[0], parts[1]
		if step == "lock" {
			switch status {
			case "fail":
				p.result.LockHeld = true
				if len(parts) >= 3 {
					if age, err := strconv.Atoi(parts[2]); err == nil && age > 0 {
						p.result.LockAgeSecs = age
					}
				}
			case "takeover":
				p.result.LockTakeover = true
			}
			return
		}
		if step == "apply" && status == "done" {
			p.result.Done = true
			return
		}
		id, _ := p.actionID(step)
		switch status {
		case "start":
			if p.hooks.OnStart != nil {
				p.hooks.OnStart(id)
			}
		case "ok":
			p.result.Completed = append(p.result.Completed, id)
			if p.hooks.OnDone != nil {
				p.hooks.OnDone(id, "ok")
			}
		case "fail":
			p.result.Failed = id
			p.result.FailedLogs = p.result.Logs[id]
			if p.hooks.OnDone != nil {
				p.hooks.OnDone(id, "fail")
			}
		}
	case strings.HasPrefix(raw, "@l "):
		parts := strings.SplitN(raw[3:], " ", 2)
		if len(parts) != 2 {
			return
		}
		id, _ := p.actionID(parts[0])
		text, err := b64dec(parts[1])
		if err != nil {
			return
		}
		logs := p.result.Logs[id]
		if len(logs) < maxKeptLogLines {
			p.result.Logs[id] = append(logs, text)
		} else {
			p.result.Logs[id] = append(logs[1:], text)
		}
		if p.hooks.OnLog != nil {
			p.hooks.OnLog(id, text)
		}
	}
}
