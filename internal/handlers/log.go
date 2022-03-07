package handlers

import (
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"
)

func httpErrorf(w http.ResponseWriter, code int, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	log.Error(msg)
	http.Error(w, msg, code)
}
