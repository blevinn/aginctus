// Command aginctus-legacy-preview produces a non-mutating, non-authorizing
// workload-to-environment candidate preview. It does not authenticate Incus,
// accept revisions, adopt resources, migrate secrets or operate on host state.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

const maxMappingFileBytes int64 = 64 << 10

func readMappings(path string) ([]environment.LegacyWorkloadMapping, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("absolute mapping file required")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil { return nil, errors.New("cannot open selected mapping file") }
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > maxMappingFileBytes {
		return nil, errors.New("mapping file must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMappingFileBytes+1))
	if err != nil || len(data) > int(maxMappingFileBytes) {
		return nil, errors.New("cannot read bounded mapping file")
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil, errors.New("mapping file must be an explicit JSON array")
	}
	out := make([]environment.LegacyWorkloadMapping, 0, len(raw))
	for _, entry := range raw {
		// Tokenize rather than unmarshalling into a map first: the latter
		// silently accepts duplicate JSON fields and case aliases.
		decoder := json.NewDecoder(bytes.NewReader(entry))
		start, err := decoder.Token()
		if err != nil || start != json.Delim('{') {
			return nil, errors.New("mapping entry must be an object")
		}
		fields := map[string]string{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || (name != "legacyId" && name != "targetId" && name != "instanceName") {
				return nil, errors.New("unknown mapping field")
			}
			if _, duplicate := fields[name]; duplicate {
				return nil, errors.New("duplicate mapping field")
			}
			var value string
			if err := decoder.Decode(&value); err != nil || value == "" {
				return nil, errors.New("invalid mapping field value")
			}
			fields[name] = value
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') || len(fields) != 3 {
			return nil, errors.New("incomplete mapping entry")
		}
		if _, err := decoder.Token(); err != io.EOF {
			return nil, errors.New("trailing mapping content")
		}
		out = append(out, environment.LegacyWorkloadMapping{
			LegacyID: fields["legacyId"], TargetID: fields["targetId"], InstanceName: fields["instanceName"],
		})
	}
	return out, nil
}

// run accepts only explicit source/target identity and mappings. No ambient
// CLI configuration, NixOS/Home Manager precedence or Incus remote alias is
// read. This preview is not an authorized migration plan.
func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("aginctus-legacy-preview", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var legacyPath, digest, deployment, project, server, id, name, mappingPath string
	fs.StringVar(&legacyPath, "legacy-path", "", "absolute legacy WorkloadDeployment JSON path")
	fs.StringVar(&digest, "expected-legacy-digest", "", "pinned SHA256 source digest")
	fs.StringVar(&deployment, "legacy-deployment", "", "exact legacy deployment ID")
	fs.StringVar(&project, "project", "", "explicit nondefault Incus project")
	fs.StringVar(&server, "server-id", "", "declared server ID, not authentication")
	fs.StringVar(&id, "environment-id", "", "target immutable environment UUID")
	fs.StringVar(&name, "environment-name", "", "target display name")
	fs.StringVar(&mappingPath, "mappings", "", "absolute JSON mapping array file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("invalid preview arguments")
	}
	if !filepath.IsAbs(legacyPath) || filepath.Clean(legacyPath) != legacyPath {
		return errors.New("absolute legacy source file required")
	}
	legacy, err := acceptedstate.ReadCandidateFile(legacyPath, deployment, project)
	if err != nil { return errors.New("invalid or mismatched legacy source") }
	mappings, err := readMappings(mappingPath)
	if err != nil { return err }
	target := environment.Descriptor{
		ID: id, Name: name,
		Binding: environment.Binding{ServerID: server, Project: project},
	}
	result, err := environment.PreviewLegacyCandidate(legacy, digest, target, mappings)
	if err != nil { return errors.New("legacy migration preview refused") }
	if stdout == nil { return errors.New("preview output required") }
	return json.NewEncoder(stdout).Encode(result)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "aginctus-legacy-preview:", err)
		os.Exit(1)
	}
}
