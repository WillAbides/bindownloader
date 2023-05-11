package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/willabides/bindown/v4/internal/bindown"
)

func Test_templateSourceListCmd(t *testing.T) {
	for _, td := range []struct {
		name   string
		config bindown.Config
		state  resultState
	}{
		{name: "no sources"},
		{
			name: "yes sources",
			config: bindown.Config{
				TemplateSources: map[string]string{
					"source1": "foo",
					"source2": "bar",
				},
			},
			state: resultState{
				stdout: "source1 foo" + "\n" + "source2 bar",
			},
		},
	} {
		t.Run(td.name, func(t *testing.T) {
			runner := newCmdRunner(t)
			runner.writeConfig(&td.config)
			result := runner.run("template-source", "list")
			result.assertState(td.state)
		})
	}
}

func Test_templateSourceAddCmd(t *testing.T) {
	for _, td := range []struct {
		name        string
		config      bindown.Config
		args        []string
		state       resultState
		wantSources map[string]string
	}{
		{
			name: "adds source",
			args: []string{"source1", "foo"},
			wantSources: map[string]string{
				"source1": "foo",
			},
		},
		{
			name: "adds source to existing sources",
			config: bindown.Config{
				TemplateSources: map[string]string{
					"source1": "foo",
				},
			},
			args: []string{"source2", "bar"},
			wantSources: map[string]string{
				"source1": "foo",
				"source2": "bar",
			},
		},
		{
			name: "duplicate source",
			config: bindown.Config{
				TemplateSources: map[string]string{
					"source1": "foo",
				},
			},
			args: []string{"source1", "bar"},
			state: resultState{
				stderr: "cmd: error: template source already exists",
				exit:   1,
			},
			wantSources: map[string]string{
				"source1": "foo",
			},
		},
	} {
		t.Run(td.name, func(t *testing.T) {
			runner := newCmdRunner(t)
			runner.writeConfig(&td.config)
			result := runner.run(append([]string{"template-source", "add"}, td.args...)...)
			result.assertState(td.state)
			config := runner.getConfigFile()
			require.Equal(t, td.wantSources, config.TemplateSources)
		})
	}
}

func Test_templateSourceRemoveCmd(t *testing.T) {
	for _, td := range []struct {
		name        string
		config      bindown.Config
		args        []string
		state       resultState
		wantSources map[string]string
	}{
		{
			name: "no sources",
			args: []string{"source1"},
			state: resultState{
				stderr: `cmd: error: no template source named "source1"`,
				exit:   1,
			},
		},
		{
			name: "remove source",
			config: bindown.Config{
				TemplateSources: map[string]string{
					"source1": "foo",
				},
			},
			args: []string{"source1"},
		},
		{
			name: "remove source with other sources",
			config: bindown.Config{
				TemplateSources: map[string]string{
					"source1": "foo",
					"source2": "bar",
				},
			},
			args: []string{"source1"},
			wantSources: map[string]string{
				"source2": "bar",
			},
		},
	} {
		t.Run(td.name, func(t *testing.T) {
			runner := newCmdRunner(t)
			runner.writeConfig(&td.config)
			result := runner.run(append([]string{"template-source", "remove"}, td.args...)...)
			result.assertState(td.state)
			config := runner.getConfigFile()
			require.Equal(t, td.wantSources, config.TemplateSources)
		})
	}
}
