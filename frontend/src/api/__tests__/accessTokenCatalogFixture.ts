// The data of GET /api/user/access_tokens/catalog (controller/access_token.go,
// service.AccessTokenCatalog), cut to a few resources. Action JSON carries no
// default roles.
export function accessTokenCatalogFixture() {
  return {
    groups: [
      {
        group: 'personal',
        resources: [
          {
            resource: 'profile',
            label_key: 'Profile',
            actions: [
              {
                action: 'read',
                label_key: 'View',
                description_key:
                  'View your profile, groups, and available models.',
              },
              {
                action: 'write',
                label_key: 'Edit',
                description_key:
                  'Change your display name and personal settings.',
              },
            ],
          },
          {
            resource: 'api_key',
            label_key: 'API keys',
            actions: [
              {
                action: 'read',
                label_key: 'View',
                description_key:
                  'View API keys and their settings without the full key.',
              },
            ],
          },
          {
            resource: 'wallet',
            label_key: 'Wallet',
            actions: [
              {
                action: 'read',
                label_key: 'View',
                description_key:
                  'View balance, top-up records, and referral code.',
              },
            ],
          },
        ],
      },
      {
        group: 'admin',
        resources: [
          {
            resource: 'channel',
            label_key: 'Channel Management',
            actions: [
              {
                action: 'secret_view',
                label_key: 'View channel secrets',
                description_key:
                  'Reserved for viewing complete channel keys after secure verification.',
              },
            ],
          },
        ],
      },
    ],
    max_tokens: 20,
    default_expiry_days: 30,
  }
}
