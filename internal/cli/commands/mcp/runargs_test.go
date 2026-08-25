package mcp_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gruntwork-io/terragrunt/internal/vfs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTFPathArgumentPicksTheImplementation pins that tf_path reaches the
// commands a tool call starts. Exec is denied, so each command is refused and
// named in the degraded list, which shows the binary the call asked for.
func TestTFPathArgumentPicksTheImplementation(t *testing.T) {
	t.Parallel()

	for _, binary := range []string{"tofu", "terraform"} {
		t.Run(binary, func(t *testing.T) {
			t.Parallel()

			dir := newTestTree(t)

			require.NoError(t, vfs.WriteFile(
				vfs.NewOSFS(), filepath.Join(dir, "a", "main.tf"), []byte(`output "id" { value = "a" }`+"\n"), 0o644))

			session := newTestSession(t, dir)

			var out renderConfigOutput

			callTool(t, session, "render_config", map[string]any{"working_dir": "b", "tf_path": binary}, &out)

			denied := `denied "` + binary + " "
			assert.True(t, slices.ContainsFunc(out.Degraded, func(note string) bool {
				return strings.HasPrefix(note, denied)
			}), "the dependency fetch must be refused as %s, got %v", binary, out.Degraded)
		})
	}
}

// TestTFPathArgumentRefusesAnythingButAnImplementationName pins that tf_path
// cannot name a file. The agent chooses the argument, and a path would let it
// choose which program runs.
func TestTFPathArgumentRefusesAnythingButAnImplementationName(t *testing.T) {
	t.Parallel()

	session := newTestSession(t, newTestTree(t))

	for _, tfPath := range []string{"/abs/path/tofu", "../tofu", "tofu.exe", "sh"} {
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name:      "render_config",
			Arguments: map[string]any{"working_dir": "b", "tf_path": tfPath},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "tf_path %q must be refused", tfPath)
	}
}

// TestRunArgumentsAreCheckedBeforeAnyoneIsAsked pins that a run given an
// argument it cannot honor fails outright, and that apply fails before it puts
// an approval in front of a person for a run that would then be refused.
func TestRunArgumentsAreCheckedBeforeAnyoneIsAsked(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args map[string]any
		name string
		tool string
	}{
		{name: "plan with a path", tool: "plan", args: map[string]any{"tf_path": "/abs/path/tofu"}},
		{name: "plan with negative parallelism", tool: "plan", args: map[string]any{"parallelism": -1}},
		{name: "apply with a path", tool: "apply", args: map[string]any{"tf_path": "/abs/path/tofu"}},
		{name: "apply with negative parallelism", tool: "apply", args: map[string]any{"parallelism": -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var prompts []string

			session := newApprovingSession(t, newTestTree(t), "accept", &prompts)

			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
			require.NoError(t, err)

			assert.True(t, res.IsError, "the run must be refused")
			assert.Empty(t, prompts, "nobody may be asked to approve a run that cannot happen")
		})
	}
}

// TestApprovalShowsTheRunArguments pins that the command line a person
// approves names the filters, the binary, and the parallelism the run will
// use, with each filter quoted as one shell word.
func TestApprovalShowsTheRunArguments(t *testing.T) {
	t.Parallel()

	var prompts []string

	session := newApprovingSession(t, newTestTree(t), "decline", &prompts)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "apply",
		Arguments: map[string]any{
			"filter":      []any{"./a", "!./b"},
			"tf_path":     "terraform",
			"parallelism": 2,
		},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError, "a declined run must not report success")

	require.Len(t, prompts, 1)
	assert.Contains(
		t,
		prompts[0],
		`terragrunt run --all apply --filter=./a --filter='!./b' --tf-path=terraform --parallelism=2`,
	)
}
