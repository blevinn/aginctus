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

func runProtectedGatewayRemove(args []string)int{
	if len(args)!=6||args[5]!="--discard-root" {
		fmt.Fprintln(os.Stderr,"usage: aginctus internal gateway-protected-remove GRANT OPERATOR_CONFIG ROLE INSTANCE_UUID CREATED_AT --discard-root")
		return 2
	}
	at,err:=time.Parse(time.RFC3339Nano,args[4])
	if err!=nil||at.IsZero(){fmt.Fprintln(os.Stderr,"invalid instance creation timestamp");return 2}
	ctx,cancel:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
	defer cancel()
	ctx,timeout:=context.WithTimeout(ctx,5*time.Minute)
	defer timeout()
	result,id,err:=gateway.RemoveLocalProtectedGatewayService(ctx,args[0],args[1],args[2],args[3],at,true)
	if id!="" {
		if json.NewEncoder(os.Stdout).Encode(map[string]any{"operationId":id,"result":result})!=nil{return 1}
	}
	if err!=nil{fmt.Fprintln(os.Stderr,"protected gateway removal failed; inspect operation before retry");return 1}
	return 0
}
