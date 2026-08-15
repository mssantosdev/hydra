package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

var (
	topicUpdateMeta      []string
	topicUpdateUnsetMeta []string
	topicUpdateForce     bool
)

// topicUpdateMaxSize bounds a document read from stdin or a file.
//
// It is a DEFAULT, not a ceiling: --max-size raises or removes it. The limit exists so a
// wrong argument (a log, a tarball, /dev/urandom) fails with a size message instead of
// filling memory, which is a different outcome from refusing work someone means to do.
const topicUpdateMaxSize = 1 << 20 // 1 MiB

var topicUpdateMaxSizeFlag int64

var topicUpdateCmd = &cobra.Command{
	Use:   "update <id> [<file>|-]",
	Short: "Set a topic's metadata and relationships",
	Long: `Update a topic in place, from flags or from a document.

Flags mutate individual keys:

  --meta key=value     set one metadata key, repeatable
  --unset-meta key     remove one, repeatable

A document REPLACES whole sections, so a file checked into a repository is the source of
truth rather than a patch with hidden merge rules. Pass a path, or - for stdin. JSON and
YAML are both accepted; only the sections present are touched:

  links:                 # replaces every outgoing relationship
    - kind: part_of
      to: epic-login
  meta:                  # replaces all metadata
    acme.pbi: "2072958"

Metadata is yours: hydra stores it, reports it in "hydra topic show", and branches on
nothing in it, so a plugin or a UI can keep its own state on the topic that owns it.

Flags and a document cannot be combined — the merge order between them would be invented
rather than obvious.`,
	Example: `  # One key
  $ hydra topic update feat-social --meta acme.pbi=2072958

  # Several, and a removal
  $ hydra topic update feat-social --meta ui.color=red --unset-meta stale.key

  # Declaratively, from a file under version control
  $ hydra topic update feat-social topics/feat-social.yaml

  # Or from a pipe
  $ echo 'meta: {acme.pbi: "2072958"}' | hydra topic update feat-social -`,
	Args:              cobra.RangeArgs(1, 2),
	RunE:              runTopicUpdate,
	ValidArgsFunction: completeTopicIDs,
}

func init() {
	topicCmd.AddCommand(topicUpdateCmd)
	topicUpdateCmd.Flags().StringArrayVar(&topicUpdateMeta, "meta", nil,
		"Set a metadata key as key=value (repeatable)")
	topicUpdateCmd.Flags().StringArrayVar(&topicUpdateUnsetMeta, "unset-meta", nil,
		"Remove a metadata key (repeatable)")
	topicUpdateCmd.Flags().BoolVar(&topicUpdateForce, "force", false,
		"Record relationships even when they close a cycle or point at themselves")
	topicUpdateCmd.Flags().Int64Var(&topicUpdateMaxSizeFlag, "max-size", topicUpdateMaxSize,
		"Maximum document size in bytes; 0 removes the limit")
}

// topicDocument is the shape `topic update` accepts.
//
// Both fields are POINTERS so "absent" and "empty" are different requests: a document with
// no `meta:` key leaves metadata alone, while `meta: {}` clears it. Without the distinction
// there would be no way to clear one section without restating the other.
type topicDocument struct {
	Links *[]topicDocumentLink `yaml:"links" json:"links"`
	Meta  *map[string]string   `yaml:"meta" json:"meta"`
}

type topicDocumentLink struct {
	Kind string `yaml:"kind" json:"kind"`
	To   string `yaml:"to" json:"to"`
}

func runTopicUpdate(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	id := strings.TrimSpace(args[0])
	if _, err := requireTopicRecorded(id); err != nil {
		return err
	}

	hasFlags := len(topicUpdateMeta) > 0 || len(topicUpdateUnsetMeta) > 0
	hasDocument := len(args) == 2

	switch {
	case hasFlags && hasDocument:
		return output.Errorf(output.CodeUsage,
			"pass fields as flags or a document, not both: a document replaces whole sections, "+
				"so combining the two would need a merge order nobody can predict").
			WithDetail("document", args[1]).
			WithDetail("flags", append(append([]string{}, topicUpdateMeta...), topicUpdateUnsetMeta...))
	case !hasFlags && !hasDocument:
		return output.Errorf(output.CodeNeedsInput,
			"nothing to update: pass --meta/--unset-meta, or a document path or - for stdin").
			WithDetail("missing", []string{"--meta", "--unset-meta", "<file>|-"}).
			WithNext(output.Next{
				Argv: []string{"hydra", "topic", "show", id, "--output", "json"},
				Why:  "see the topic's current relationships and metadata",
			})
	case hasDocument:
		declared, err := applyTopicDocument(cmd, id, args[1])
		if err != nil {
			return err
		}
		// A document that declares NO section is a valid no-op, distinct from an empty file:
		// `{}` is a caller — usually a template — saying "no changes", and it must converge at
		// exit 0 rather than being an error. An empty FILE is a different thing and stays
		// needs_input, because a truncated document or a wrong path is a mistake, not a request.
		if !declared {
			return emitTopicUpdate(cmd, id, "nothing to update")
		}
	default:
		set, err := parseMetaAssignments(topicUpdateMeta)
		if err != nil {
			return err
		}
		if err := topicStore().UpdateMeta(id, set, topicUpdateUnsetMeta); err != nil {
			return classifyTopicErr(err)
		}
	}

	return emitTopicUpdate(cmd, id, "")
}

// applyTopicDocument reads the document and applies the sections it declares, reporting whether
// it declared any.
func applyTopicDocument(cmd *cobra.Command, id, arg string) (bool, error) {
	// "-" names stdin and a path names a file, exactly as `hydra apply` does: an agent that
	// execs without a shell cannot redirect, so stdin-only would force every non-shell
	// caller to plumb a pipe for a file it already has on disk.
	source := cmd.InOrStdin()
	from := "stdin"
	if arg != "-" {
		f, err := os.Open(arg) //nolint:gosec // G304: a document path is the command's argument
		if err != nil {
			return false, output.Wrap(output.CodeNeedsInput, err,
				"topic update could not read %q; pass a readable file or - for stdin", arg).
				WithDetail("argument", arg).
				WithDetail("missing", []string{arg})
		}
		defer func() { _ = f.Close() }()
		source, from = f, arg
	}

	doc, err := readTopicDocument(source, from)
	if err != nil {
		return false, err
	}

	store := topicStore()
	if doc.Links != nil {
		links := make([]topic.Link, 0, len(*doc.Links))
		for _, l := range *doc.Links {
			links = append(links, topic.Link{Kind: strings.TrimSpace(l.Kind), To: strings.TrimSpace(l.To)})
		}
		if err := store.ReplaceLinks(id, links, topicUpdateForce); err != nil {
			return false, withForceNext(classifyTopicErr(err), "topic", "update", id, arg)
		}
	}
	if doc.Meta != nil {
		if err := store.ReplaceMeta(id, *doc.Meta); err != nil {
			return false, classifyTopicErr(err)
		}
	}
	return doc.Links != nil || doc.Meta != nil, nil
}

// readTopicDocument decodes JSON or YAML from one reader.
//
// One decoder for both: JSON is a subset of what yaml.v3 parses, so sniffing the format
// would add a branch that can disagree with itself for no gain.
func readTopicDocument(r io.Reader, from string) (topicDocument, error) {
	var doc topicDocument
	limited := io.Reader(r)
	if topicUpdateMaxSizeFlag > 0 {
		// One byte past the limit, so hitting it is distinguishable from a document that
		// ends exactly at it.
		limited = io.LimitReader(r, topicUpdateMaxSizeFlag+1)
	}
	raw, err := io.ReadAll(limited)
	if err != nil {
		return doc, output.Wrap(output.CodeIOFailed, err, "failed to read %s", from)
	}
	if topicUpdateMaxSizeFlag > 0 && int64(len(raw)) > topicUpdateMaxSizeFlag {
		return doc, output.Errorf(output.CodeUsage,
			"%s is larger than the %d byte limit; raise it with --max-size N or remove it with --max-size 0",
			from, topicUpdateMaxSizeFlag).
			WithDetail("source", from).
			WithDetail("limit_bytes", topicUpdateMaxSizeFlag)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return doc, output.Errorf(output.CodeNeedsInput, "%s was empty", from).
			WithDetail("missing", []string{from}).
			WithDetail("reason", "expected a document with a links: or meta: section")
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return doc, output.Wrap(output.CodeUsage, err,
			"%s is not valid JSON or YAML", from).
			WithDetail("source", from)
	}
	return doc, nil
}

// parseMetaAssignments splits key=value pairs, keeping everything after the FIRST separator
// so a value may contain one.
func parseMetaAssignments(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, output.Errorf(output.CodeUsage,
				"--meta %q is not key=value", pair).
				WithDetail("argument", pair).
				WithDetail("expected", "key=value")
		}
		out[strings.TrimSpace(key)] = value
	}
	return out, nil
}

// emitTopicUpdate reports the topic as it now stands, the way a resource-shaped API answers a
// write: the caller sees the result instead of having to ask for it in a second invocation.
//
// summary overrides the default when the update was a declared no-op, so a caller can tell
// "nothing to do" from "done" without diffing the payload.
func emitTopicUpdate(cmd *cobra.Command, id, summary string) error {
	updated, err := requireTopicRecorded(id)
	if err != nil {
		return err
	}
	payload := describeTopic(updated)
	noop := summary != ""
	if summary == "" {
		summary = topicShowSummary(payload)
	}
	return emit(cmd, summary, payload, nil, func() {
		fmt.Println()
		if noop {
			fmt.Println(styles.Success.Render("✓ Nothing to update"))
		} else {
			fmt.Println(styles.Success.Render("✓ Topic updated"))
		}
		fmt.Printf("  %s\n", styles.Label.Render(payload.ID))
		fmt.Println()
		printTopicGraph(payload)
	})
}
