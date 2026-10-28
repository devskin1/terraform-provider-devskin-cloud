package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Varios endpoints de criacao respondem antes do recurso existir de fato: a
// VLAN, a VM, o banco e o cluster levam de 1 a 3 minutos para ficar prontos e
// responder so no fim estouraria o limite de ~100s do Cloudflare. O provider
// lia a resposta do POST na hora e gravava campos computed vazios (ip,
// endpoint, porta, subnet padrao), quebrando qualquer recurso que dependesse
// deles no mesmo apply. Daqui em diante o Create espera o provisionamento
// terminar antes de fechar o estado.

const recursoProvisionTimeout = 6 * time.Minute

// var, nao const: o teste encurta o intervalo para nao esperar em tempo real.
var recursoPollInterval = 5 * time.Second

// errEsperaEstourou marca o prazo vencido, que e tratado como aviso e nao como
// falha: o recurso existe e costuma terminar sozinho. Derrubar o apply aqui
// transformaria um backend lento em regressao pior que o bug original.
var errEsperaEstourou = errors.New("prazo de espera estourado")

// emProvisionamento diz se vale a pena continuar esperando. A lista e de
// estados transitorios, nao de estados prontos: assim um status novo ou
// desconhecido encerra a espera em vez de travar o apply por 6 minutos.
func emProvisionamento(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CREATING", "PENDING", "PROVISIONING", "STARTING", "UPDATING", "MODIFYING":
		return true
	}
	return false
}

// recursoFoiDestruido diz se o backend ja considera o recurso morto.
//
// Instancia, banco e load balancer NAO somem da tabela quando sao apagados
// pelo painel: viram TERMINATED e o GET por id segue respondendo 200 com o
// registro inteiro. O Read so tirava do estado em 404, entao o Terraform
// continuava achando que o recurso existia — `terraform refresh` nao acusava
// nada e o apply seguinte nao recriava nada. Visto em 28/10 na Awise: VM
// apagada pelo painel, state intacto, plan vazio, sem jeito de recriar.
//
// TERMINATING entra junto porque e so o caminho ate TERMINATED: quem ja
// mandou destruir nao vai voltar atras, e esperar o status final so adiaria
// a recriacao para o apply seguinte.
func recursoFoiDestruido(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "TERMINATED", "TERMINATING", "DELETED", "DELETING":
		return true
	}
	return false
}

func provisionamentoFalhou(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ERROR", "FAILED":
		return true
	}
	return false
}

// waitForResourceReady faz polling no recurso ate ele sair do provisionamento.
// Devolve sempre o payload mais recente que conseguiu ler, inclusive quando o
// prazo estoura, para o chamador poder fechar o estado com o que existe.
func waitForResourceReady(ctx context.Context, client *ApiClient, caminho, id string, timeout time.Duration) (map[string]interface{}, error) {
	if id == "" {
		return nil, errors.New("o recurso foi criado sem id")
	}
	limite := time.Now().Add(timeout)
	var ultimo map[string]interface{}
	for {
		body, status, err := client.Get(fmt.Sprintf("%s/%s", caminho, id))
		if err != nil {
			return ultimo, fmt.Errorf("lendo %s/%s: %w", caminho, id, err)
		}
		switch {
		case status >= 200 && status < 300:
			var parsed map[string]interface{}
			if err := json.Unmarshal(body, &parsed); err != nil {
				return ultimo, fmt.Errorf("resposta invalida ao ler %s/%s: %w", caminho, id, err)
			}
			ultimo = unwrapData(parsed)
			atual := getString(ultimo, "status")
			if provisionamentoFalhou(atual) {
				return ultimo, fmt.Errorf("o provisionamento terminou com status %s; verifique o recurso no painel antes de repetir o apply", atual)
			}
			if !emProvisionamento(atual) {
				return ultimo, nil
			}
		case status == 404:
			// leitura correndo na frente da escrita logo apos o POST
		default:
			return ultimo, fmt.Errorf("lendo %s/%s: status %d: %s", caminho, id, status, string(body))
		}

		if time.Now().After(limite) {
			return ultimo, fmt.Errorf("%w: ainda em %q depois de %s", errEsperaEstourou, getString(ultimo, "status"), timeout)
		}
		select {
		case <-ctx.Done():
			return ultimo, ctx.Err()
		case <-time.After(recursoPollInterval):
		}
	}
}

// aguardarRecurso embrulha waitForResourceReady no tratamento de diagnostico.
// Devolve o payload a aplicar no estado, ou nil quando nao ha nada novo.
// O chamador deve gravar o estado de qualquer jeito: mesmo em falha o recurso
// existe no backend, e deixa-lo fora do estado criaria um orfao que o destroy
// nao alcanca.
func aguardarRecurso(ctx context.Context, client *ApiClient, caminho, id, rotulo string, diags *diag.Diagnostics) map[string]interface{} {
	data, err := waitForResourceReady(ctx, client, caminho, id, recursoProvisionTimeout)
	switch {
	case err == nil:
		return data
	case errors.Is(err, errEsperaEstourou):
		diags.AddWarning(
			fmt.Sprintf("%s ainda esta sendo provisionado", rotulo),
			fmt.Sprintf("%v. O recurso foi criado e provavelmente termina sozinho; "+
				"rode `terraform refresh` em seguida para atualizar os campos calculados.", err))
		return data
	default:
		diags.AddError(fmt.Sprintf("Falha ao aguardar %s", rotulo), err.Error())
		return data
	}
}
