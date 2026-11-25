package authz

const ResourceAnnouncement = "announcement"

var (
	AnnouncementRead  = Permission{Resource: ResourceAnnouncement, Action: ActionRead}
	AnnouncementWrite = Permission{Resource: ResourceAnnouncement, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceAnnouncement,
		LabelKey: "Announcement management",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "Read announcements",
				DescriptionKey: "View announcements, schedules, and user read status.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Manage announcements",
				DescriptionKey: "Create, edit, publish, archive, and delete announcements.",
				DefaultRoles:   []string{BuiltInRoleAdmin},
			},
		},
	})
}
