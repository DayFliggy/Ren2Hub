import { passwordFeedback } from '../../passwordFeedback'
export default {
  passwordFeedback: passwordFeedback['en'],
  common: {
    search: 'Search',
    all: 'All',
    actions: 'Actions',
    confirm: 'Confirm',
    cancel: 'Cancel',
    delete: 'Delete',
    edit: 'Edit',
    save: 'Save',
    create: 'Create',
    export: 'Export',
    copy: 'Copy',
    copied: 'Copied',
    enabled: 'Enabled',
    disabled: 'Disabled',
    viewMore: 'View more',
    loading: 'Loading…',
    refresh: 'Refresh',
    retry: 'Retry',
    loadFailed: 'Failed to load',
    publicationPending:
      'Changes were saved. Runtime configuration publication is pending retry.',
    versionUpdate: {
      message:
        'The site has been updated. Reload the page to use the new version.',
      reload: 'Reload page',
    },
    appError: {
      label: 'Application error',
      title: 'This page could not be rendered',
      message:
        'An unexpected interface error interrupted this page. Retry the view or return home.',
      retry: 'Retry view',
      home: 'Return home',
    },
    clientErrors: {
      oauthPopupBlocked:
        'The browser blocked the sign-in pop-up. Allow pop-ups and try again.',
      oauthProviderUnavailable:
        'This sign-in method is not configured. Choose another method or contact the administrator.',
      oauthInvalidAuthorizationUrl:
        'The sign-in provider returned an invalid authorization address.',
      oauthCancelled: 'Third-party sign-in was cancelled.',
      passkeyCancelled: 'Passkey verification was cancelled.',
      pluginLoginInvalidResponse:
        'The plugin returned an invalid login step. Start the login again.',
      pluginLoginInvalidFlow:
        'The plugin did not start a login session. Start the login again.',
      pluginLoginInvalidChannel:
        'The login callback did not identify a valid channel.',
      checkoutInvalidUrl:
        'The payment service returned an invalid checkout address. No payment page was opened.',
      checkoutInvalidForm:
        'The payment service returned an invalid checkout form. No payment page was opened.',
    },
    navigationError: {
      title: 'This page could not be displayed',
      message:
        'The page failed to load because of a temporary network or service problem. Retry, or return home.',
      rateLimitedTitle: 'Too many requests',
      rateLimitedMessage:
        'This network has reached its request limit. Try again later, or return home for now.',
      home: 'Return home',
    },
    tableRows: 'Table rows',
    none: 'No data',
    prevPage: 'Prev',
    nextPage: 'Next',
    pageInfo: '{total} items total',
    pageSize: 'Show {size}',
    itemsPerPage: 'Items per page',
    pageProgress: 'Page {page} of {count}',
    goToPage: 'Go to page {page}',
    never: 'Never expires',
    unlimited: 'Unlimited',
    back: 'Back',
    close: 'Close',
    breadcrumb: 'Breadcrumb',
    removeItem: 'Remove {item}',
    showBalance: 'Show balance',
    hideBalance: 'Hide balance',
    decreaseValue: 'Decrease {item}',
    increaseValue: 'Increase {item}',
    imagePreview: 'Image preview',
    logout: 'Sign out',
    signedOut: 'Signed out',
    success: 'Success',
    failed: 'Failed',
    invalidInput: 'Enter a valid value.',
    pending: 'Pending',
    copyFailed: 'Copy failed — please copy manually',
    selectAll: 'Select all',
    selectRow: 'Select row {index}',
    selectColumn: 'Select',
    to: 'to',
    noResults: 'No results',
    unsavedChanges: 'Unsaved changes',
    turnstileRequired: 'Complete the security verification first.',
    turnstileUnavailable:
      'Security verification is unavailable. Refresh the page and try again.',
  },
}
