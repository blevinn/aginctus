package openai

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestOAuthStateProtectedRoundTrip(t *testing.T) {
    t.Setenv("XDG_STATE_HOME", t.TempDir())
    dir,err:=stateDirectory()
    if err!=nil{t.Fatal(err)}
    id,err:=ensureHostID(dir)
    if err!=nil{t.Fatal(err)}
    if !strings.HasPrefix(id,"urn:uuid:"){t.Fatalf("unexpected host id")}
    again,err:=ensureHostID(dir)
    if err!=nil || again!=id{t.Fatal("host id changed")}
    session:=&Session{
        ClientID:"oaiapp_test",HostID:id,Issuer:issuer,Subject:"subject",
        IDToken:"example-id-token",AccessToken:"SECRET_ACCESS",RefreshToken:"SECRET_REFRESH",
    }
    if err:=saveSession(dir,session);err!=nil{t.Fatal(err)}
    info,err:=os.Stat(filepath.Join(dir,"session.json"))
    if err!=nil{t.Fatal(err)}
    if info.Mode().Perm()!=0600{t.Fatalf("session mode = %o",info.Mode().Perm())}
    actual,err:=loadSession(dir)
    if err!=nil{t.Fatal(err)}
    if actual.ClientID!=session.ClientID || actual.RefreshToken!=session.RefreshToken {
        t.Fatal("persisted credentials changed")
    }
}

func TestOAuthStateRejectsSymlinksAndWorldReadableFiles(t *testing.T) {
    dir:=t.TempDir()
    outside:=filepath.Join(dir,"outside")
    if err:=os.WriteFile(outside,[]byte("sensitive"),0600);err!=nil{t.Fatal(err)}
    link:=filepath.Join(dir,"session.json")
    if err:=os.Symlink(outside,link);err!=nil{t.Fatal(err)}
    if _,err:=loadSession(dir);err==nil{t.Fatal("symlink was accepted")}
    if err:=os.Remove(link);err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(link,[]byte("{}"),0644);err!=nil{t.Fatal(err)}
    if _,err:=loadSession(dir);err==nil{t.Fatal("world-readable session was accepted")}
}

func TestOAuthRefusesMalformedIDTokenWithoutFetchingKeys(t *testing.T) {
    _,err:=verifyIDToken(context.Background(),nil,"not-a-token","oaiapp_123","nonce")
    if err==nil{t.Fatal("malformed id token accepted")}
}

func TestOAuthStateRejectsInvalidPersistedHostID(t *testing.T) {
    dir:=t.TempDir()
    if err:=os.WriteFile(filepath.Join(dir,"host-id"),[]byte("not-a-valid-id"),0600);err!=nil{t.Fatal(err)}
    if _,err:=ensureHostID(dir);err==nil{t.Fatal("invalid host ID accepted")}
}
