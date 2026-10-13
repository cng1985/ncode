package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func paging(c *gin.Context, page, size int) (int, int, bool) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || size < 1 || size > 200 {
		failure(c, http.StatusBadRequest, 41001, "page must be positive and size must be between 1 and 200", nil)
		return 0, 0, false
	}
	return page, size, true
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func internalFailure(c *gin.Context) {
	failure(c, http.StatusInternalServerError, 50001, "request could not be completed", nil)
}
