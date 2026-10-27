package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Reproduz o bug relatado pelo cliente em 27/10/2026: POST /networking/vpcs
// responde 202 com status CREATING e `subnets` vazio, e a subnet padrao so
// aparece 1 a 3 minutos depois. Antes do wait, o Create lia a resposta do POST
// e deixava default_subnet_id vazio, quebrando a kubmix_instance do mesmo apply.
func servidorFalso(t *testing.T, respostas []string) (*ApiClient, *int) {
	t.Helper()
	chamadas := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/networking/vpcs/vpc-1" {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
		}
		i := chamadas
		chamadas++
		if i >= len(respostas) {
			i = len(respostas) - 1
		}
		corpo := respostas[i]
		if corpo == "404" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"success":false}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, corpo)
	}))
	t.Cleanup(ts.Close)
	return &ApiClient{BaseURL: ts.URL, Token: "t", HTTPClient: ts.Client()}, &chamadas
}

const criando = `{"success":true,"data":{"id":"vpc-1","name":"awise","status":"CREATING","cidrBlock":"10.0.150.0/24","subnets":[]}}`
const pronta = `{"success":true,"data":{"id":"vpc-1","name":"awise","status":"RUNNING","cidrBlock":"10.0.150.0/24","subnets":[{"id":"subnet-9","name":"awise-subnet-1"}]}}`
const comErro = `{"success":true,"data":{"id":"vpc-1","name":"awise","status":"ERROR","cidrBlock":"10.0.150.0/24","subnets":[]}}`

func TestEsperaAteFicarPronta(t *testing.T) {
	vpcPollInterval = time.Millisecond
	cli, chamadas := servidorFalso(t, []string{criando, criando, pronta})
	data, err := waitForVPCReady(context.Background(), cli, "vpc-1", 5*time.Second)
	if err != nil {
		t.Fatalf("nao devia falhar: %v", err)
	}
	if got := getString(data, "status"); got != "RUNNING" {
		t.Fatalf("status = %q, queria RUNNING", got)
	}
	subs, ok := data["subnets"].([]interface{})
	if !ok || len(subs) != 1 {
		t.Fatalf("a subnet padrao nao voltou: %#v", data["subnets"])
	}
	if *chamadas < 3 {
		t.Fatalf("devia ter feito polling, fez %d chamada(s)", *chamadas)
	}
}

func TestFalhaQuandoVpcVaiParaErro(t *testing.T) {
	vpcPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{criando, comErro})
	if _, err := waitForVPCReady(context.Background(), cli, "vpc-1", 5*time.Second); err == nil {
		t.Fatal("status ERROR tinha que virar erro, nao sucesso silencioso")
	}
}

func TestToleraO404LogoDepoisDo202(t *testing.T) {
	vpcPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{"404", criando, pronta})
	if _, err := waitForVPCReady(context.Background(), cli, "vpc-1", 5*time.Second); err != nil {
		t.Fatalf("404 inicial e corrida de leitura, nao erro: %v", err)
	}
}

func TestEstouraOPrazoSemTravarParaSempre(t *testing.T) {
	vpcPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{criando})
	_, err := waitForVPCReady(context.Background(), cli, "vpc-1", 50*time.Millisecond)
	if err == nil {
		t.Fatal("tinha que estourar o prazo")
	}
}
