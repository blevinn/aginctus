package testenv

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	nativeInstanceScenario = "native-instance"
	nativeScenarioUnit = "aginctus-testenv-native-instance.service"
	nativeScenarioTTL = 35 * time.Minute
	nativeScenarioTimeout = 30 * time.Minute
	systemctlGuest = "/run/current-system/sw/bin/systemctl"
)

type nativeScenarioState struct {
	load, active, sub, result, code, status string
}

// Parse a bounded fixed systemctl show response, not a guest-authored
// arbitrary success marker. The image and its fixture must be independently
// reviewed and pinned; guest root can still forge guest-side observations.
func parseNativeScenarioState(output string) (nativeScenarioState, error) {
	var s nativeScenarioState
	fields := map[string]*string{
		"LoadState": &s.load, "ActiveState": &s.active, "SubState": &s.sub,
		"Result": &s.result, "ExecMainCode": &s.code, "ExecMainStatus": &s.status,
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		dest, known := fields[key]
		if !ok || !known || value == "" || *dest != "" {
			return nativeScenarioState{}, errors.New("invalid native scenario state response")
		}
		*dest = value
	}
	for _, dest := range fields {
		if *dest == "" {
			return nativeScenarioState{}, errors.New("incomplete native scenario state response")
		}
	}
	if s.load != "loaded" {
		return nativeScenarioState{}, errors.New("native scenario unit unavailable")
	}
	return s, nil
}

func readNativeScenarioState(ctx context.Context, b VMBackend, ref VMRef) (nativeScenarioState, error) {
	out, err := b.Exec(ctx, ref, []string{systemctlGuest, "show",
		"--property=LoadState", "--property=ActiveState", "--property=SubState",
		"--property=Result", "--property=ExecMainCode", "--property=ExecMainStatus",
		nativeScenarioUnit}, 10*time.Second)
	if err != nil || out.ExitCode != 0 || out.Stderr != "" {
		return nativeScenarioState{}, errors.New("native scenario state unavailable")
	}
	return parseNativeScenarioState(out.Stdout)
}

// runNativeInstanceScenario uses only the signed guest execution capability.
// Launch once and poll bounded guest unit state; never retry an uncertain
// launch or widen the per-exec deadline. VM destruction is the caller's
// independent cleanup path even after a cancelled or failed poll.
func runNativeInstanceScenario(ctx context.Context, b VMBackend, ref VMRef) error {
	ctx, cancel := context.WithTimeout(ctx, nativeScenarioTimeout)
	defer cancel()
	initial, err := readNativeScenarioState(ctx, b, ref)
	if err != nil || initial.active != "inactive" || initial.sub != "dead" ||
		initial.result != "success" || initial.code != "0" || initial.status != "0" {
		return errors.New("native scenario requires a fresh inactive fixture unit")
	}
	out, err := b.Exec(ctx, ref, []string{systemctlGuest, "start", "--no-block", nativeScenarioUnit}, 10*time.Second)
	if err != nil || out.ExitCode != 0 || out.Stdout != "" || out.Stderr != "" {
		return errors.New("native scenario launch unverified")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state, err := readNativeScenarioState(ctx, b, ref)
		if err != nil {
			return err
		}
		if state.active == "active" && state.sub == "exited" &&
			state.result == "success" && state.code == "1" && state.status == "0" {
			return ctx.Err()
		}
		if state.result != "success" || (state.active != "activating" && state.active != "inactive") {
			return errors.New("native scenario did not complete successfully")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
