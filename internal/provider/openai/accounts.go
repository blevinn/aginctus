package openai

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "syscall"
 "time"
)

var accountIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func accountID(s *Session) string {
 digest := sha256.Sum256([]byte(s.ClientID + "\x00" + s.Subject))
 return hex.EncodeToString(digest[:])
}

func accountsDir(dir string) (string,error) {
 path := filepath.Join(dir,"accounts")
 if err:=os.MkdirAll(path,0700);err!=nil{return "",err}
 info,err:=os.Lstat(path)
 if err!=nil{return "",err}
 if !info.IsDir() || info.Mode().Perm()&0077!=0{return "",errors.New("OAuth accounts directory must be private")}
 return path,nil
}

func accountPath(dir,id string)(string,error){
 if !accountIDPattern.MatchString(id){return "",errors.New("invalid account identifier")}
 path,err:=accountsDir(dir)
 if err!=nil{return "",err}
 return filepath.Join(path,id+".json"),nil
}

func readAccount(dir,id string)(*Session,error){
 path,err:=accountPath(dir,id)
 if err!=nil{return nil,err}
 data,err:=readProtected(path)
 if err!=nil{return nil,err}
 var s Session
 if err:=json.Unmarshal(data,&s);err!=nil{return nil,errors.New("invalid OAuth account record")}
 if accountID(&s)!=id || s.ClientID=="" || s.Subject==""{return nil,errors.New("OAuth account identity mismatch")}
 return &s,nil
}

func persistAccount(dir string,s *Session) error {
 if s.ClientID=="" || s.Subject=="" {return errors.New("incomplete OAuth account identity")}
 path,err:=accountPath(dir,accountID(s))
 if err!=nil{return err}
 data,err:=json.MarshalIndent(s,"","  ")
 if err!=nil{return err}
 return atomicWrite(path,data)
}

func activeAccountID(dir string)(string,error){
 b,err:=readProtected(filepath.Join(dir,"active-account"))
 if err!=nil{return "",err}
 id:=strings.TrimSpace(string(b))
 if !accountIDPattern.MatchString(id){return "",errors.New("invalid active OAuth account")}
 return id,nil
}

func selectAccount(dir,id string) error {
 if _,err:=readAccount(dir,id);err!=nil{return err}
 return atomicWrite(filepath.Join(dir,"active-account"),[]byte(id+"\n"))
}

// migrateLegacyLocked transfers the single-session record from early PR #170
// without discarding its client registration. Caller must hold .account.lock.
func migrateLegacyLocked(dir string)error{
 legacy,err:=loadSession(dir)
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 if legacy.ClientID=="" || legacy.Subject==""{return errors.New("cannot migrate incomplete legacy OAuth registration")}
 id:=accountID(legacy)
 path,err:=accountPath(dir,id)
 if err!=nil{return err}
 if _,err:=readProtected(path);errors.Is(err,os.ErrNotExist){
  if err:=persistAccount(dir,legacy);err!=nil{return err}
 }else if err!=nil{return err}
 if _,err:=activeAccountID(dir);errors.Is(err,os.ErrNotExist){
  if err:=selectAccount(dir,id);err!=nil{return err}
 }else if err!=nil{return err}
 // Delete only after the credential record and active pointer are durable.
 return os.Remove(filepath.Join(dir,"session.json"))
}

func activeSession(dir string)(*Session,error){
 id,err:=activeAccountID(dir)
 if err==nil{return readAccount(dir,id)}
 if !errors.Is(err,os.ErrNotExist){return nil,err}
 // Read-only compatibility for credentials saved by PR #170 before
 // account-scoped storage was introduced.
 return loadSession(dir)
}

func storedAccounts(dir string)([]string,error){
 accounts,err:=accountsDir(dir)
 if err!=nil{return nil,err}
 entries,err:=os.ReadDir(accounts)
 if err!=nil{return nil,err}
 ids:=make([]string,0,len(entries))
 for _,e:=range entries {
  name:=e.Name()
  if !strings.HasSuffix(name,".json"){continue}
  id:=strings.TrimSuffix(name,".json")
  if !accountIDPattern.MatchString(id){return nil,errors.New("invalid OAuth account filename")}
  if _,err:=readAccount(dir,id);err!=nil{return nil,err}
  ids=append(ids,id)
 }
 return ids,nil
}

// WithAccountLock holds a lock across all operations which could rotate
// the selected account's refresh token. Other callers must use this lock.
func withAccountLock(dir string,do func()error)error{
 file,err:=os.OpenFile(filepath.Join(dir,".account.lock"),os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW,0600)
 if err!=nil{return err}
 defer file.Close()
 if err:=syscall.Flock(int(file.Fd()),syscall.LOCK_EX);err!=nil{return err}
 defer syscall.Flock(int(file.Fd()),syscall.LOCK_UN)
 return do()
}

// Status prints account metadata only; never credentials or ID tokens.
func Status(out io.Writer)error{
 dir,err:=stateDirectory()
 if err!=nil{return err}
 ids,err:=storedAccounts(dir)
 if err!=nil{return err}
 active,_:=activeAccountID(dir)
 if len(ids)==0{
  if legacy,err:=loadSession(dir);err==nil{
   fmt.Fprintf(out,"legacy account: %s (migration required; not gateway-provisioned)\n",legacy.Email)
   return nil
  }else if !errors.Is(err,os.ErrNotExist){return err}
  fmt.Fprintln(out,"No OpenAI OAuth account registrations")
  return nil
 }
 for _,id:=range ids{
  s,err:=readAccount(dir,id)
  if err!=nil{return err}
  marker:=" "
  if id==active{marker="*"}
  condition:="signed out"
  if s.AccessToken!=""{
   condition="access token expired"
   if time.Now().Before(s.SavedAt.Add(time.Duration(s.ExpiresIn)*time.Second)){
    condition="access token present (not remotely verified)"
   }
  }
  fmt.Fprintf(out,"%s %s %s %s\n",marker,id[:12],s.Email,condition)
 }
 fmt.Fprintln(out,"Gateway provisioning: not configured")
 return nil
}

func Select(id string)error{
 dir,err:=stateDirectory()
 if err!=nil{return err}
 return withAccountLock(dir,func()error{
  if err:=migrateLegacyLocked(dir);err!=nil{return err}
  return selectAccount(dir,id)
 })
}

func Logout(ctx context.Context,out io.Writer)error{
 dir,err:=stateDirectory()
 if err!=nil{return err}
 return withAccountLock(dir,func()error{
  if err:=migrateLegacyLocked(dir);err!=nil{return err}
  id,err:=activeAccountID(dir)
  if err!=nil{return err}
  s,err:=readAccount(dir,id)
  if err!=nil{return err}
  revoked:=s.RefreshToken==""
  var reason string
  if !revoked{
   revoked,reason=revokeRefresh(ctx,&http.Client{Timeout:15*time.Second},s)
  }
  // Keep registration and identity to permit re-sign-in with client ID.
  s.AccessToken="";s.RefreshToken="";s.IDToken=""
  s.ExpiresIn=0;s.SavedAt=time.Time{}
  if err:=persistAccount(dir,s);err!=nil{return err}
  if revoked{
   fmt.Fprintln(out,"Local OAuth session cleared; remote refresh token revocation confirmed.")
  }else{
   fmt.Fprintf(out,"Local OAuth session cleared; remote revocation NOT confirmed (%s). Disconnect this app in ChatGPT Settings if needed.\n",reason)
  }
  return nil
 })
}

func revokeRefresh(ctx context.Context,client *http.Client,s *Session)(bool,string){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,issuer+"/.well-known/openid-configuration",nil)
 if err!=nil{return false,"discovery request invalid"}
 response,err:=client.Do(req)
 if err!=nil{return false,"discovery network error"}
 defer response.Body.Close()
 if response.StatusCode!=200{return false,fmt.Sprintf("discovery HTTP %d",response.StatusCode)}
 var discovery struct{RevocationEndpoint string `json:"revocation_endpoint"`}
 if json.NewDecoder(io.LimitReader(response.Body,64<<10)).Decode(&discovery)!=nil{return false,"invalid discovery response"}
 endpoint,err:=url.Parse(discovery.RevocationEndpoint)
 if err!=nil || endpoint.Scheme!="https" || endpoint.Host!="auth.openai.com" || endpoint.User!=nil || endpoint.Fragment!="" {
  return false,"unsafe revocation endpoint"
 }
 form:=url.Values{"token":{s.RefreshToken},"token_type_hint":{"refresh_token"},"client_id":{s.ClientID}}
 req,err=http.NewRequestWithContext(ctx,http.MethodPost,endpoint.String(),strings.NewReader(form.Encode()))
 if err!=nil{return false,"invalid revocation request"}
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 result,err:=client.Do(req)
 if err!=nil{return false,"revocation network error"}
 defer result.Body.Close()
 if result.StatusCode!=200{return false,fmt.Sprintf("revocation HTTP %d",result.StatusCode)}
 return true,""
}

func Refresh(ctx context.Context,out io.Writer)error{
 dir,err:=stateDirectory()
 if err!=nil{return err}
 return withAccountLock(dir,func()error{
  id,err:=activeAccountID(dir)
  if err!=nil{return err}
  s,err:=readAccount(dir,id)
  if err!=nil{return err}
  if s.RefreshToken==""{return errors.New("active account is signed out; reauthorize with --oauth")}
  if time.Now().Before(s.SavedAt.Add(time.Duration(s.ExpiresIn)*time.Second-3*time.Minute)){
   fmt.Fprintln(out,"Access token remains current (local expiry check only)")
   return nil
  }
  if err:=refreshSession(ctx,&http.Client{Timeout:15*time.Second},tokenURL,s);err!=nil{return err}
  // Persist the rotating refresh token atomically under the account lock.
  if err:=persistAccount(dir,s);err!=nil{return err}
  fmt.Fprintln(out,"OpenAI OAuth session refreshed and saved")
  return nil
 })
}

func refreshSession(ctx context.Context,client *http.Client,endpoint string,s *Session)error{
 if s.ClientID=="" || s.RefreshToken==""{return errors.New("refresh requires an issued client and renewable session")}
 form:=url.Values{
  "grant_type":{"refresh_token"},"client_id":{s.ClientID},
  "refresh_token":{s.RefreshToken},"resource":{resource},
 }
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,endpoint,strings.NewReader(form.Encode()))
 if err!=nil{return err}
 request.Header.Set("Content-Type","application/x-www-form-urlencoded")
 res,err:=client.Do(request)
 if err!=nil{return fmt.Errorf("OpenAI refresh request: %w",err)}
 defer res.Body.Close()
 if res.StatusCode!=200{return fmt.Errorf("OpenAI refresh failed (HTTP %d); previous session preserved",res.StatusCode)}
 var tokens tokenReply
 if err:=json.NewDecoder(io.LimitReader(res.Body,64<<10)).Decode(&tokens);err!=nil{return errors.New("invalid OpenAI refresh response")}
 if tokens.AccessToken=="" || tokens.RefreshToken=="" || !strings.EqualFold(tokens.TokenType,"Bearer") || tokens.ExpiresIn<=0{
  return errors.New("incomplete OpenAI token refresh response")
 }
 scopes:=strings.Fields(tokens.Scope)
 if len(scopes)>0{
  allowed:=false
  for _,scope:=range scopes{if scope=="chatgpt.tokens.use.direct"{allowed=true}}
  if !allowed{return errors.New("refreshed access was not granted direct ChatGPT usage scope")}
 }
 // No new ID token is substituted until its signature and claims can be
 // independently validated; the original verified identity is retained.
 s.AccessToken=tokens.AccessToken;s.RefreshToken=tokens.RefreshToken;s.ExpiresIn=tokens.ExpiresIn
 s.TokenType=tokens.TokenType;s.SavedAt=time.Now().UTC()
 if len(scopes)>0{s.Scopes=scopes}
 return nil
}
