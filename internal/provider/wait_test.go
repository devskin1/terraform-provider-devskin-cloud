package provider

import (
	"context"
	"errors"
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
	recursoPollInterval = time.Millisecond
	cli, chamadas := servidorFalso(t, []string{criando, criando, pronta})
	data, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 5*time.Second)
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
	recursoPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{criando, comErro})
	if _, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 5*time.Second); err == nil {
		t.Fatal("status ERROR tinha que virar erro, nao sucesso silencioso")
	}
}

func TestToleraO404LogoDepoisDo202(t *testing.T) {
	recursoPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{"404", criando, pronta})
	if _, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 5*time.Second); err != nil {
		t.Fatalf("404 inicial e corrida de leitura, nao erro: %v", err)
	}
}

func TestEstouraOPrazoSemTravarParaSempre(t *testing.T) {
	recursoPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{criando})
	_, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 50*time.Millisecond)
	if err == nil {
		t.Fatal("tinha que estourar o prazo")
	}
}

// Status desconhecido tem que encerrar a espera, nao travar 6 minutos: a lista
// que o helper conhece e a dos estados transitorios, nao a dos estados prontos.
func TestStatusDesconhecidoEncerraAEspera(t *testing.T) {
	recursoPollInterval = time.Millisecond
	const estranho = `{"success":true,"data":{"id":"vpc-1","status":"ALGO_NOVO","subnets":[]}}`
	cli, chamadas := servidorFalso(t, []string{estranho})
	if _, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 2*time.Second); err != nil {
		t.Fatalf("status desconhecido nao e erro: %v", err)
	}
	if *chamadas != 1 {
		t.Fatalf("devia parar na primeira leitura, fez %d", *chamadas)
	}
}

// AVAILABLE e o estado pronto de banco de dados; se o helper so aceitasse
// RUNNING, todo kubmix_database esperaria ate estourar o prazo.
func TestAvailableContaComoPronto(t *testing.T) {
	recursoPollInterval = time.Millisecond
	const disponivel = `{"success":true,"data":{"id":"vpc-1","status":"AVAILABLE","endpoint":"db.interno","port":5432}}`
	cli, _ := servidorFalso(t, []string{criando, disponivel})
	data, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 2*time.Second)
	if err != nil {
		t.Fatalf("AVAILABLE e pronto: %v", err)
	}
	if getString(data, "endpoint") != "db.interno" {
		t.Fatalf("devia devolver o payload mais novo, veio %#v", data)
	}
}

// No estouro de prazo o chamador ainda precisa do ultimo payload para fechar o
// estado, e o erro precisa ser reconhecivel como aviso e nao como falha.
func TestPrazoEstouradoDevolvePayloadESentinela(t *testing.T) {
	recursoPollInterval = time.Millisecond
	cli, _ := servidorFalso(t, []string{criando})
	data, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "vpc-1", 30*time.Millisecond)
	if !errors.Is(err, errEsperaEstourou) {
		t.Fatalf("queria a sentinela de prazo, veio %v", err)
	}
	if getString(data, "id") != "vpc-1" {
		t.Fatalf("payload do ultimo polling tinha que vir junto, veio %#v", data)
	}
}

func TestSemIdNaoFazPolling(t *testing.T) {
	cli, chamadas := servidorFalso(t, []string{pronta})
	if _, err := waitForResourceReady(context.Background(), cli, "/networking/vpcs", "", time.Second); err == nil {
		t.Fatal("id vazio tem que ser erro imediato")
	}
	if *chamadas != 0 {
		t.Fatalf("nao devia chamar a API sem id, chamou %d", *chamadas)
	}
}
