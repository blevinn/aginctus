package openai

import (
    "context"
    "crypto"
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/rsa"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "math/big"
    "net"
    "net/http"
    "net/url"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "sync/atomic"
    "syscall"
    "time"
)

const (
    issuer = "https://auth.openai.com"
    authURL = issuer + "/api/accounts/authorize"
    tokenURL = issuer + "/api/accounts/oauth/token"
    keysURL = issuer + "/.well-known/jwks.json"
    resource = "https://api.openai.com/v1"
    scopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
    callbackPath = "/auth/callback"
)

type Session struct {
    Email string `json:"email"`
    Issuer string `json:"issuer"`
    Subject string `json:"subject"`
    ClientID string `json:"client_id"`
    HostID string `json:"ext_agent_host_id"`
    IDToken string `json:"id_token"`
    AccessToken string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
    TokenType string `json:"token_type"`
    ExpiresIn int64 `json:"expires_in"`
    Scopes []string `json:"scopes"`
    SavedAt time.Time `json:"saved_at"`
}

type tokenReply struct {
    AccessToken string `json:"access_token"`
    RefreshToken string `json:"refresh_token"`
    IDToken string `json:"id_token"`
    TokenType string `json:"token_type"`
    ExpiresIn int64 `json:"expires_in"`
    Scope string `json:"scope"`
}

type oauthCallback struct {
    Code, ClientID, Error string
}

func Login(ctx context.Context, output io.Writer) error {
    dir, err := stateDirectory()
    if err != nil { return err }
    hostID, err := ensureHostID(dir)
    if err != nil { return err }
    existing, err := loadSession(dir)
    if err != nil && !errors.Is(err, os.ErrNotExist) { return err }

    listener, err := net.Listen("tcp4", "127.0.0.1:0")
    if err != nil { return fmt.Errorf("open local OAuth callback listener: %w",err) }
    defer listener.Close()

    state, err := randomString(32)
    if err != nil { return err }
    nonce, err := randomString(32)
    if err != nil { return err }
    verifier, err := randomString(48)
    if err != nil { return err }
    challenge := sha256.Sum256([]byte(verifier))
    redirect := "http://" + listener.Addr().String() + callbackPath

    cid := "dynamic_agent_client"
    if existing != nil {
        if existing.HostID != hostID || existing.ClientID == "" { return errors.New("saved OAuth registration belongs to another host") }
        cid = existing.ClientID
    }
    params := url.Values{
        "client_id": {cid},
        "ext_agent_host_id": {hostID},
        "response_type": {"code"},
        "redirect_uri": {redirect},
        "scope": {scopes},
        "resource": {resource},
        "state": {state},
        "nonce": {nonce},
        "code_challenge_method": {"S256"},
        "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])},
    }
    if existing == nil {
        params.Set("agent_name_hint", "Aginctus")
    }
    address := authURL+"?"+params.Encode()

    callback := make(chan oauthCallback, 1)
    server := &http.Server{ReadHeaderTimeout: 5*time.Second}
    var handled atomic.Bool
    mux := http.NewServeMux()
    mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet || r.URL.Path != callbackPath {
            http.Error(w,"invalid callback",http.StatusBadRequest);return
        }
        if r.URL.Query().Get("state") != state {
            http.Error(w,"invalid authorization state",http.StatusBadRequest);return
        }
        if !handled.CompareAndSwap(false,true) {
            http.Error(w,"authorization callback already received",http.StatusConflict);return
        }
        c := oauthCallback{
            Code:r.URL.Query().Get("code"),
            ClientID:r.URL.Query().Get("client_id"),
            Error:r.URL.Query().Get("error"),
        }
        select { case callback <- c: default: }
        w.Header().Set("Content-Type","text/plain; charset=utf-8")
        fmt.Fprintln(w,"You may return to Aginctus. Authorization is being validated.")
    })
    server.Handler = mux
    done := make(chan struct{})
    go func() { _ = server.Serve(listener); close(done) }()
    defer func() { _ = server.Close(); <-done }()

    fmt.Fprintln(output,"Continue with ChatGPT in your browser:")
    fmt.Fprintln(output,address)
    // A missing graphical session is normal on headless NixOS hosts; the URL
    // above can be opened manually on the same host.
    if opener:=exec.Command("xdg-open",address); opener.Start()==nil {
        go func(){_ = opener.Wait()}()
    }

    timeout, cancel := context.WithTimeout(ctx,5*time.Minute)
    defer cancel()
    var received oauthCallback
    select {
    case received = <-callback:
    case <-timeout.Done():
        return fmt.Errorf("OpenAI OAuth authorization did not complete: %w",timeout.Err())
    }
    if received.Error != "" { return fmt.Errorf("OpenAI authorization denied: %s",received.Error) }
    if received.Code == "" { return errors.New("OAuth callback did not contain an authorization code") }
    issuedID := cid
    if existing == nil {
        if !strings.HasPrefix(received.ClientID,"oaiapp_") { return errors.New("OpenAI did not return a valid issued client ID") }
        issuedID = received.ClientID
    } else if received.ClientID != "" && received.ClientID != cid {
        return errors.New("OpenAI returned an unexpected client ID")
    }
    client := &http.Client{Timeout:15*time.Second}
    form := url.Values{
        "grant_type":{"authorization_code"},
        "client_id":{issuedID},
        "code":{received.Code},
        "redirect_uri":{redirect},
        "code_verifier":{verifier},
        "resource":{resource},
    }
    request, err := http.NewRequestWithContext(timeout,http.MethodPost,tokenURL,strings.NewReader(form.Encode()))
    if err != nil { return err }
    request.Header.Set("Content-Type","application/x-www-form-urlencoded")
    res, err := client.Do(request)
    if err != nil { return fmt.Errorf("exchange OpenAI authorization code: %w",err) }
    defer res.Body.Close()
    if res.StatusCode != http.StatusOK {
        return fmt.Errorf("OpenAI token exchange failed (HTTP %d)",res.StatusCode)
    }
    var tokens tokenReply
    if err := json.NewDecoder(io.LimitReader(res.Body,64<<10)).Decode(&tokens); err != nil {
        return fmt.Errorf("decode OpenAI token response: %w",err)
    }
    if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.IDToken == "" || !strings.EqualFold(tokens.TokenType,"Bearer") {
        return errors.New("OpenAI token response missing required credentials")
    }
    if !strings.Contains(" "+tokens.Scope+" "," chatgpt.tokens.use.direct ") {
        return errors.New("the ChatGPT plan usage scope was not granted")
    }
    identity, err := verifyIDToken(timeout,client,tokens.IDToken,issuedID,nonce)
    if err != nil { return fmt.Errorf("verify OpenAI identity: %w",err) }
    if existing != nil && (existing.Subject != identity.Subject || existing.Issuer != identity.Issuer) {
        return errors.New("reauthorization returned a different account; existing session preserved")
    }
    if identity.Subject == "" { return errors.New("OpenAI ID token has no subject") }
    session := Session{
        Email:identity.Email,Issuer:identity.Issuer,Subject:identity.Subject,ClientID:issuedID,
        HostID:hostID,IDToken:tokens.IDToken,AccessToken:tokens.AccessToken,
        RefreshToken:tokens.RefreshToken,TokenType:tokens.TokenType,
        ExpiresIn:tokens.ExpiresIn,Scopes:strings.Fields(tokens.Scope),SavedAt:time.Now().UTC(),
    }
    if err := saveSession(dir,&session); err != nil { return err }
    fmt.Fprintln(output,"Aginctus OpenAI OAuth authorization saved securely for the current user.")
    return nil
}

func stateDirectory() (string,error) {
    base := os.Getenv("XDG_STATE_HOME")
    if base == "" {
        home,err := os.UserHomeDir()
        if err != nil { return "",err }
        base = filepath.Join(home,".local","state")
    }
    dir := filepath.Join(base,"aginctus","providers","openai")
    if err := os.MkdirAll(dir,0700); err != nil { return "",err }
    info,err:=os.Lstat(dir)
    if err!=nil{return "",err}
    if !info.IsDir() {return "",errors.New("OAuth state path must be a non-symlink directory")}
    if err := os.Chmod(dir,0700); err != nil { return "",err }
    return dir,nil
}

func randomString(n int) (string,error) {
    b := make([]byte,n)
    if _,err := rand.Read(b); err != nil { return "",err }
    return base64.RawURLEncoding.EncodeToString(b),nil
}

func ensureHostID(dir string) (string,error) {
    path := filepath.Join(dir,"host-id")
    b,err := readProtected(path)
    if err == nil {
        id := strings.TrimSpace(string(b))
        if !strings.HasPrefix(id,"urn:uuid:") { return "",errors.New("invalid persisted OAuth host ID") }
        return id,nil
    }
    if !errors.Is(err,os.ErrNotExist) { return "",err }
    raw := make([]byte,16)
    if _,err := rand.Read(raw);err != nil{return "",err}
    raw[6] = (raw[6]&0x0f)|0x40
    raw[8] = (raw[8]&0x3f)|0x80
    id := fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x",raw[:4],raw[4:6],raw[6:8],raw[8:10],raw[10:])
    if err := atomicWrite(path,[]byte(id+"\n"));err != nil{return "",err}
    return id,nil
}

func loadSession(dir string) (*Session,error) {
    raw,err := readProtected(filepath.Join(dir,"session.json"))
    if err != nil { return nil,err }
    var session Session
    if err := json.Unmarshal(raw,&session);err != nil { return nil,errors.New("stored OpenAI OAuth session is invalid") }
    return &session,nil
}

func saveSession(dir string,session *Session) error {
    raw,err := json.MarshalIndent(session,"","  ")
    if err != nil{return err}
    return atomicWrite(filepath.Join(dir,"session.json"),raw)
}

func readProtected(path string) ([]byte,error) {
    stat,err := os.Lstat(path)
    if err != nil{return nil,err}
    if !stat.Mode().IsRegular() || stat.Mode().Perm()&0077 != 0 {
        return nil,errors.New("OAuth state must be a regular file with owner-only permissions")
    }
    f,err := os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW,0)
    if err != nil{return nil,err}
    defer f.Close()
    opened,err:=f.Stat()
    if err!=nil{return nil,err}
    if !opened.Mode().IsRegular() || opened.Mode().Perm()&0077!=0 {
        return nil,errors.New("OAuth state changed while opening")
    }
    return io.ReadAll(io.LimitReader(f,1<<20))
}

func atomicWrite(path string,data []byte) error {
    dir := filepath.Dir(path)
    file,err := os.CreateTemp(dir,".oauth-state-*")
    if err != nil{return err}
    defer os.Remove(file.Name())
    if err := file.Chmod(0600);err != nil{file.Close();return err}
    if _,err := file.Write(data);err != nil{file.Close();return err}
    if err := file.Sync();err != nil{file.Close();return err}
    if err := file.Close();err != nil{return err}
    return os.Rename(file.Name(),path)
}

type idClaims struct {
    Issuer string `json:"iss"`
    Subject string `json:"sub"`
    Email string `json:"email"`
    Nonce string `json:"nonce"`
    Audience json.RawMessage `json:"aud"`
    ExpiresAt int64 `json:"exp"`
}

type jwk struct {
    Kty string `json:"kty"`
    Kid string `json:"kid"`
    Alg string `json:"alg"`
    N string `json:"n"`
    E string `json:"e"`
    Crv string `json:"crv"`
    X string `json:"x"`
    Y string `json:"y"`
}
func verifyIDToken(ctx context.Context,client *http.Client,token,clientID,nonce string)(idClaims,error){
    var claim idClaims
    chunks:=strings.Split(token,".")
    if len(chunks)!=3 {return claim,errors.New("malformed ID token")}
    headerRaw,err:=base64.RawURLEncoding.DecodeString(chunks[0])
    if err!=nil{return claim,errors.New("invalid ID token header")}
    var header struct{Alg string `json:"alg"`;Kid string `json:"kid"`}
    if err:=json.Unmarshal(headerRaw,&header);err!=nil{return claim,errors.New("invalid ID token header")}
    if header.Kid=="" || (header.Alg!="RS256" && header.Alg!="ES256") {
        return claim,errors.New("unsupported OpenAI ID token signature algorithm")
    }
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,keysURL,nil)
    if err!=nil{return claim,err}
    res,err:=client.Do(req)
    if err!=nil{return claim,fmt.Errorf("fetch OpenAI signing keys: %w",err)}
    defer res.Body.Close()
    if res.StatusCode!=200{return claim,fmt.Errorf("signing-key fetch failed (HTTP %d)",res.StatusCode)}
    var keys struct{ Keys []jwk `json:"keys"` }
    if err:=json.NewDecoder(io.LimitReader(res.Body,1<<20)).Decode(&keys);err!=nil{return claim,err}
    sig,err:=base64.RawURLEncoding.DecodeString(chunks[2])
    if err!=nil{return claim,errors.New("invalid ID token signature")}
    signed:=[]byte(chunks[0]+"."+chunks[1])
    sum:=sha256.Sum256(signed)
    verified:=false
    for _,k:=range keys.Keys{
        if k.Kid!=header.Kid {continue}
        switch {
        case header.Alg=="RS256" && k.Kty=="RSA":
            nBytes,eBytes:=decodeRaw(k.N),decodeRaw(k.E)
            if len(nBytes)==0 || len(eBytes)==0{continue}
            exponent:=new(big.Int).SetBytes(eBytes)
            if !exponent.IsInt64() || exponent.Int64()<3 || exponent.Int64()>1<<31{continue}
            key:=&rsa.PublicKey{N:new(big.Int).SetBytes(nBytes),E:int(exponent.Int64())}
            verified=rsa.VerifyPKCS1v15(key,crypto.SHA256,sum[:],sig)==nil
        case header.Alg=="ES256" && k.Kty=="EC" && k.Crv=="P-256" && len(sig)==64:
            x,y:=new(big.Int).SetBytes(decodeRaw(k.X)),new(big.Int).SetBytes(decodeRaw(k.Y))
            verified=ecdsa.Verify(&ecdsa.PublicKey{Curve:elliptic.P256(),X:x,Y:y},sum[:],new(big.Int).SetBytes(sig[:32]),new(big.Int).SetBytes(sig[32:]))
        }
        if verified{break}
    }
    if !verified{return claim,errors.New("OpenAI ID token signature verification failed")}
    payload,err:=base64.RawURLEncoding.DecodeString(chunks[1])
    if err!=nil{return claim,errors.New("invalid ID token payload")}
    if err:=json.Unmarshal(payload,&claim);err!=nil{return claim,errors.New("invalid ID token claims")}
    if claim.Issuer!=issuer || claim.Nonce!=nonce || claim.ExpiresAt<=time.Now().Unix() {
        return claim,errors.New("OpenAI ID token issuer, nonce, or expiration mismatch")
    }
    var audience string
    if err:=json.Unmarshal(claim.Audience,&audience);err==nil {
        if audience!=clientID{return claim,errors.New("OpenAI ID token audience mismatch")}
    }else{
        var audiences []string
        if err:=json.Unmarshal(claim.Audience,&audiences);err!=nil{return claim,errors.New("invalid ID token audience")}
        matched:=false
        for _,v:=range audiences{if v==clientID{matched=true}}
        if !matched{return claim,errors.New("OpenAI ID token audience mismatch")}
    }
    return claim,nil
}
func decodeRaw(s string) []byte {b,_:=base64.RawURLEncoding.DecodeString(s);return b}

