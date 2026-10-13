package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type envelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

type pageEnvelope struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	Data         any    `json:"data"`
	Page         int    `json:"page"`
	Size         int    `json:"size"`
	Total        int64  `json:"total"`
	TotalPage    int64  `json:"totalPage"`
	ResponseType string `json:"responseType"`
}

func success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, envelope{Code: 200, Msg: "success", Data: data})
}

func pageSuccess(c *gin.Context, data any, page, size int, total int64) {
	pages := int64(0)
	if total > 0 {
		pages = (total + int64(size) - 1) / int64(size)
	}
	c.JSON(http.StatusOK, pageEnvelope{Code: 200, Msg: "success", Data: data, Page: page, Size: size, Total: total, TotalPage: pages, ResponseType: "page"})
}

func failure(c *gin.Context, status, code int, msg string, data any) {
	c.JSON(status, envelope{Code: code, Msg: msg, Data: data})
}

type apiError struct {
	status int
	code   int
	msg    string
	data   any
}

func badRequest(c *gin.Context) { failure(c, http.StatusBadRequest, 41001, "invalid request", nil) }
