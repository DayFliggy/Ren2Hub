package authz

const ResourceUpstream = "upstream"

var (
	UpstreamRead           = Permission{Resource: ResourceUpstream, Action: ActionRead}
	UpstreamOperate        = Permission{Resource: ResourceUpstream, Action: ActionOperate}
	UpstreamWrite          = Permission{Resource: ResourceUpstream, Action: ActionWrite}
	UpstreamSensitiveWrite = Permission{Resource: ResourceUpstream, Action: ActionSensitiveWrite}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceUpstream, LabelKey: "Upstream management", Actions: []ActionDefinition{
		{Action: ActionRead, LabelKey: "Read upstream accounts", DescriptionKey: "View account snapshots, masked keys, and channel bindings.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: ActionOperate, LabelKey: "Sync upstream accounts", DescriptionKey: "Refresh account snapshots and reconcile channel bindings.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: ActionWrite, LabelKey: "Edit upstream accounts", DescriptionKey: "Edit account settings, remote key settings, and channel bindings.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: ActionSensitiveWrite, LabelKey: "Manage upstream credentials", DescriptionKey: "Manage account credentials, endpoints, and remote key creation or deletion."},
	}})
}
