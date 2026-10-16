package gateway

import (
    "context"
    "fmt"
    "net/url"

    incusclient "github.com/lxc/incus/v7/client"
    "github.com/lxc/incus/v7/shared/api"
)

const (
    legacyPostgresHost = "postgres:5432"
    desiredPostgresHost = "postgres-1:5432"
)

// reconcileDatabaseURL only changes the database hostname. Never include
// its value in errors: it contains the runtime database password.
func reconcileDatabaseURL(raw string) (string, bool, error) {
    u, err := url.Parse(raw)
    if err != nil || u == nil || u.Scheme != "postgresql" ||
        u.User == nil || u.User.Username() != "litellm" ||
        u.Path != "/litellm" || u.RawQuery != "" || u.Fragment != "" {
        return "", false, fmt.Errorf("unexpected LiteLLM database connection format")
    }
    if _, hasPassword := u.User.Password(); !hasPassword {
        return "", false, fmt.Errorf("LiteLLM database password is missing")
    }
    switch u.Host {
    case desiredPostgresHost:
        return raw, false, nil
    case legacyPostgresHost:
        u.Host = desiredPostgresHost
        return u.String(), true, nil
    default:
        return "", false, fmt.Errorf("LiteLLM database hostname is not managed by this repair")
    }
}

func reconcileDatabaseInstance(inst *api.Instance, project string) (api.InstancePut, bool, error) {
    if err := ownedLiteLLM(inst, project); err != nil {
        return api.InstancePut{}, false, err
    }
    if inst.Status != "Stopped" {
        return api.InstancePut{}, false, fmt.Errorf("database repair requires stopped LiteLLM instance")
    }
    if inst.Config["user.label.incus-compose.service"] != "litellm" {
        return api.InstancePut{}, false, fmt.Errorf("target is not LiteLLM Compose service")
    }
    corrected, changed, err := reconcileDatabaseURL(inst.Config["environment.DATABASE_URL"])
    if err != nil {
        return api.InstancePut{}, false, err
    }
    put := inst.Writable()
    put.Config = make(map[string]string, len(inst.Config))
    for key, value := range inst.Config { put.Config[key] = value }
    if changed {
        put.Config["environment.DATABASE_URL"] = corrected
    }
    return put, changed, nil
}

// RepairLiteLLMDatabaseHost is intentionally a one-time, narrowly scoped
// in-place edit. It neither starts a guest nor reconciles other environment
// variables, and leaves attached volumes and gateway secrets untouched.
func RepairLiteLLMDatabaseHost(ctx context.Context, project string) (bool, error) {
    if !gatewayIDPattern.MatchString(project) || project == "default" {
        return false, fmt.Errorf("invalid isolated gateway Compose project")
    }
    conn, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
    if err != nil {
        return false, fmt.Errorf("connect local Incus: %w", err)
    }
    p, _, err := conn.GetProject(project)
    if err != nil {
        return false, fmt.Errorf("read gateway project: %w", err)
    }
    if p.Config["user.aginctus.managed"] != "true" ||
        p.Config["user.aginctus.resource"] != "compose-project" ||
        p.Config["user.aginctus.deployment"] != project {
        return false, fmt.Errorf("gateway project ownership does not match")
    }
    client := conn.UseProject(project)
    inst, etag, err := client.GetInstance(liteLLMServiceInstance)
    if err != nil {
        return false, fmt.Errorf("read LiteLLM instance: %w", err)
    }
    put, changed, err := reconcileDatabaseInstance(inst, project)
    if err != nil {
        return false, err
    }
    if !changed {
        return false, nil
    }
    op, err := client.UpdateInstance(liteLLMServiceInstance, put, etag)
    if err != nil {
        return false, fmt.Errorf("Incus rejected LiteLLM hostname-only update (details suppressed to protect credential values)")
    }
    if err := op.WaitContext(ctx); err != nil {
        return false, fmt.Errorf("LiteLLM hostname update operation failed (details suppressed to protect credential values)")
    }
    return true, nil
}
