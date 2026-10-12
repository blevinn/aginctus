package main

import (
 "context"
 "errors"
 "flag"
 "fmt"
 "net/http"
 "os"
 "os/signal"
 "syscall"
 "time"

 "github.com/blevinn/aginctus/internal/oauthgateway"
)

func main(){
 if err:=run();err!=nil{
  fmt.Fprintln(os.Stderr,"oauth gateway:",err)
  os.Exit(1)
 }
}

func run()error{
 listen:=flag.String("listen","127.0.0.1:4081","loopback-only liveness address")
 state:=flag.String("state-dir",oauthgateway.StatePath,"private OAuth credential volume mountpoint")
 flag.Parse()
 // The service cannot run without its private pre-mounted credential volume.
 // Never create or silently fall back to a writable rootfs directory.
 if err:=oauthgateway.CheckStateDir(*state);err!=nil{return err}
 listener,err:=oauthgateway.ListenLoopback(*listen)
 if err!=nil{return err}
 defer listener.Close()
 server:=&http.Server{
  Handler:oauthgateway.Handler(),
  ReadHeaderTimeout:5*time.Second,
  MaxHeaderBytes:8192,
 }
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
 defer stop()
 go func(){
  <-ctx.Done()
  shutdown,release:=context.WithTimeout(context.Background(),5*time.Second)
  defer release()
  _=server.Shutdown(shutdown)
 }()
 err=server.Serve(listener)
 if errors.Is(err,http.ErrServerClosed){return nil}
 return err
}
