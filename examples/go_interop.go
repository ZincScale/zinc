//go:build ignore

package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

//line examples/go_interop.zn:11
func main() {
//line examples/go_interop.zn:13
	u := url.URL{Scheme: "https", Host: "example.com", Path: "/api"}
//line examples/go_interop.zn:14
	fmt.Println(u.String())
//line examples/go_interop.zn:18
	s := http.Server{Addr: ":8443", TLSConfig: &tls.Config{MinVersion: 3}}
//line examples/go_interop.zn:22
	fmt.Println(s.Addr)
//line examples/go_interop.zn:25
	r := strings.NewReplacer("hello", "hi", "world", "earth")
//line examples/go_interop.zn:26
	fmt.Println(r.Replace("hello world"))
}
