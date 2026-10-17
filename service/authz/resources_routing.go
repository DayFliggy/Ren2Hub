package authz

const ResourceRouting = "routing"

var (
	RoutingRead  = Permission{Resource: ResourceRouting, Action: ActionRead}
	RoutingWrite = Permission{Resource: ResourceRouting, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceRouting, LabelKey: "Token routing", Actions: []ActionDefinition{
		{Action: ActionRead, LabelKey: "View own token routing", DescriptionKey: "View routing preferences and eligible channels for your own tokens.", DefaultRoles: []string{BuiltInRoleUser, BuiltInRoleAdmin}},
		{Action: ActionWrite, LabelKey: "Edit own token routing", DescriptionKey: "Choose an ordered subset of authorized channels for your own tokens.", DefaultRoles: []string{BuiltInRoleUser, BuiltInRoleAdmin}},
	}})
}
