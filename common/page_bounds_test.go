package common

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPageQueryBoundsEveryAliasAndOffset(t *testing.T) {
	for _, query := range []string{"page_size=-1", "ps=-1", "size=-1", "p=-2&page_size=10", "p=9223372036854775807&page_size=100", "page_size=101"} {
		t.Run(query, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/?"+query, nil)
			p := GetPageQuery(c)
			require.GreaterOrEqual(t, p.GetPage(), 1)
			require.GreaterOrEqual(t, p.GetPageSize(), 1)
			require.LessOrEqual(t, p.GetPageSize(), 100)
			require.GreaterOrEqual(t, p.GetStartIdx(), 0)
			require.GreaterOrEqual(t, p.GetEndIdx(), p.GetStartIdx())
			require.LessOrEqual(t, p.GetPage(), math.MaxInt/p.GetPageSize())
		})
	}
}
