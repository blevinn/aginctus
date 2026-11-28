package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/gateway"
)

func runProtectedGatewayStageRuntime(args []string) int {
	if len(args)!=2 {
		fmt.Fprintln(os.Stderr,"usage: aginctus internal gateway-protected-stage-runtime GRANT OPERATOR_CONFIG")
		return 2
	}
	ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
	defer stop()
	ctx,cancel:=context.WithTimeout(ctx,5*time.Minute)
	defer cancel()
	id,err:=gateway.StageLocalProtectedGatewayRuntimeMetadata(ctx,args[0],args[1])
	if id!="" {
		if json.NewEncoder(os.Stdout).Encode(map[string]any{"operationId":id,"staged":err==nil})!=nil{return 1}
	}
	if err!=nil {
		fmt.Fprintln(os.Stderr,"protected gateway runtime staging failed")
		return 1
	}
	return 0
}
