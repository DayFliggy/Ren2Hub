package common

import "math"

// Bound both page size and multiplication before a query reaches GORM: a
// negative limit disables pagination, and an overflowing offset wraps negative.
func normalizePageBounds(page *PageInfo) {
	if page.PageSize <= 0 {
		page.PageSize = max(1, ItemsPerPage)
	}
	page.PageSize = min(page.PageSize, 100)
	page.Page = min(max(page.Page, 1), math.MaxInt/page.PageSize)
}
