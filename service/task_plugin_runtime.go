package service

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	hosttypes "github.com/QuantumNous/new-api/types"
)

var taskPluginVersions sync.Map

func LoadTaskPluginExact(ref hosttypes.TaskPluginRef) (*jsplugin.LoadedPlugin, error) {
	key := ref.Key + "@" + ref.Version + ":" + ref.SourceHash
	if cached, ok := taskPluginVersions.Load(key); ok {
		return cached.(*jsplugin.LoadedPlugin), nil
	}
	row, err := model.GetTaskPluginExact(ref)
	if err != nil {
		return nil, err
	}
	plugin, err := jsplugin.CompilePlugin(string(row.Source), jsplugin.Options{Key: ref.Key, Version: ref.Version})
	if err != nil {
		return nil, err
	}
	if plugin.Meta.Key != ref.Key || plugin.Meta.Version != ref.Version || plugin.SourceHash != ref.SourceHash {
		return nil, errors.New("plugin reference does not match executable metadata")
	}
	actual, _ := taskPluginVersions.LoadOrStore(key, plugin)
	return actual.(*jsplugin.LoadedPlugin), nil
}

func LoadTaskPluginForTask(task *model.Task) (*jsplugin.LoadedPlugin, error) {
	if task == nil || task.PrivateData.PluginRef == nil {
		return nil, errors.New("task has no immutable plugin reference")
	}
	return LoadTaskPluginExact(*task.PrivateData.PluginRef)
}
