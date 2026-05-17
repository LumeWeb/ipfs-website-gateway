package main

import (
	_ "github.com/alexliesenfeld/health"
	_ "go.lumeweb.com/configmanager"
	_ "github.com/hashicorp/golang-lru/v2"
	_ "github.com/ipfs/boxo/path"
	_ "github.com/labstack/echo/v4"
	_ "github.com/miekg/dns"
	_ "github.com/urfave/cli/v3"
	_ "go.lumeweb.com/ipfs-sdk"
	_ "go.uber.org/zap"
)

func main() {}
