//go:build darwin

package httpc

import (
	"reflect"
	"testing"
)

const scutilProxySample = `<dictionary> {
  ExceptionsList : <array> {
    0 : *.local
    1 : 169.254/16
    2 : <local>
  }
  ExcludeSimpleHostnames : 1
  HTTPEnable : 1
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7897
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 0
  SOCKSPort : 0
  SOCKSProxy : 
}
`

func TestParseSCUtilProxyReadsHTTPAndHTTPS(t *testing.T) {
	got, err := parseSCUtilProxy(scutilProxySample)
	if err != nil {
		t.Fatalf("parseSCUtilProxy: %v", err)
	}
	if !got.Enabled {
		t.Fatal("a proxy with HTTP and HTTPS enabled is reported as off")
	}
	if want := "http=127.0.0.1:7897;https=127.0.0.1:7897"; got.Server != want {
		t.Errorf("server = %q, want %q", got.Server, want)
	}
	if want := []string{"*.local", "169.254/16", "<local>"}; !reflect.DeepEqual(got.Bypass, want) {
		t.Errorf("bypass = %v, want %v", got.Bypass, want)
	}
}

func TestParseSCUtilProxyReadsSOCKS(t *testing.T) {
	got, err := parseSCUtilProxy(`<dictionary> {
  SOCKSEnable : 1
  SOCKSPort : 1080
  SOCKSProxy : ::1
}`)
	if err != nil {
		t.Fatalf("parseSCUtilProxy: %v", err)
	}
	if want := "socks=[::1]:1080"; got.Server != want {
		t.Errorf("server = %q, want %q", got.Server, want)
	}
}

func TestParseSCUtilProxyLeavesADisabledProxyOff(t *testing.T) {
	got, err := parseSCUtilProxy(`<dictionary> {
  HTTPEnable : 0
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 0
  SOCKSEnable : 0
}`)
	if err != nil {
		t.Fatalf("parseSCUtilProxy: %v", err)
	}
	if got.Enabled || got.Server != "" {
		t.Errorf("disabled proxy = %+v, want nothing", got)
	}
}

// 仅有 PAC 的配置不是手动代理：本文件不解析 PAC，因此结果为「无」，请求走直连。
func TestParseSCUtilProxyDoesNotInventAProxyFromPAC(t *testing.T) {
	got, err := parseSCUtilProxy(`<dictionary> {
  ProxyAutoConfigEnable : 1
  ProxyAutoConfigURLString : http://example.test/proxy.pac
}`)
	if err != nil {
		t.Fatalf("parseSCUtilProxy: %v", err)
	}
	if got.Enabled || got.Server != "" {
		t.Errorf("PAC-only proxy = %+v, want nothing", got)
	}
}
