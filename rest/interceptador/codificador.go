package interceptador

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/rafaeljusto/atiradorfrequente/núcleo/erros"
	"github.com/registrobr/gostk/log"
	"github.com/registrobr/gostk/reflect"
)

// filtroImagemCorpo é a expressão regular que identifica imagens no corpo da
// requisição ou da resposta em formato JSON. Utilizado para filtrar o tamanho
// excessivo dos campos de imagem.
var filtroImagemCorpo = regexp.MustCompile(`"imagem[^"]*":( )?"[^"]*"`)

type codificador interface {
	Field(string, string) interface{}
	Logger() log.Logger
	Req() *http.Request
	ResponseWriter() http.ResponseWriter
}

// Codificador popula o objeto da requisição a partir do JSON recebido na rede,
// também é responsável por criar o JSON a partir do objeto da resposta.
type Codificador struct {
	handler      codificador
	tipoConteúdo string
}

// NovoCodificador cria um novo interceptador Codificador.
func NovoCodificador(c codificador, tipoConteúdo string) *Codificador {
	return &Codificador{handler: c, tipoConteúdo: tipoConteúdo}
}

// Before traduz do formato JSON para o objeto da requisição no handler.
func (c *Codificador) Before() int {
	c.handler.Logger().Debug("Interceptador Antes: Codificador")

	método := strings.ToLower(c.handler.Req().Method)
	campoRequisição := c.handler.Field("request", método)

	if campoRequisição == nil {
		// não foi identificada nenhuma requisição
		return 0
	}

	var buffer bytes.Buffer
	tee := io.TeeReader(c.handler.Req().Body, &buffer)
	decodificador := json.NewDecoder(tee)

	for {
		if err := decodificador.Decode(campoRequisição); err != nil {
			if err == io.EOF {
				break
			}

			c.handler.Logger().Error(erros.Novo(err))
			return http.StatusInternalServerError
		}
	}

	// TODO(rafaeljusto): Tratar casos de login para não exibir as senha nos logs

	requisiçãoCorpo := strings.TrimSpace(strings.Replace(buffer.String(), "\n", "", -1))
	requisiçãoCorpo = filtrarImagemCorpo(requisiçãoCorpo)
	c.handler.Logger().Debugf("Requisição corpo: “%s”", requisiçãoCorpo)
	return 0
}

// After gera o JSON e cabeçalhos HTTP a partir do objeto de resposta.
func (c *Codificador) After(códigoHTTP int) int {
	c.handler.Logger().Debug("Interceptador Depois: Codificador")

	if campoCabeçalho := c.handler.Field("response", "header"); campoCabeçalho != nil {
		if cabeçalho, ok := campoCabeçalho.(*http.Header); ok {
			for chave, valores := range *cabeçalho {
				for _, valor := range valores {
					c.handler.ResponseWriter().Header().Add(chave, valor)
				}
			}
		} else {
			c.handler.Logger().Errorf("“Cabeçalho” campo com tipo errado: %T", campoCabeçalho)
		}
	}

	var resposta interface{}
	método := strings.ToLower(c.handler.Req().Method)

	respostaGenérica := c.handler.Field("response", "all")
	if reflect.IsDefined(respostaGenérica) {
		resposta = respostaGenérica
	} else if respostaEspecífica := c.handler.Field("response", método); respostaEspecífica != nil {
		resposta = respostaEspecífica
	}

	if resposta == nil {
		c.handler.ResponseWriter().WriteHeader(códigoHTTP)
		return códigoHTTP
	}

	c.handler.ResponseWriter().Header().Set("Content-Type", c.tipoConteúdo)
	c.handler.ResponseWriter().WriteHeader(códigoHTTP)

	defer func() {
		var buffer bytes.Buffer
		w := io.MultiWriter(c.handler.ResponseWriter(), &buffer)
		if err := json.NewEncoder(w).Encode(resposta); err != nil {
			c.handler.Logger().Error(erros.Novo(err))
			return
		}

		respostaCorpo := strings.TrimSpace(strings.Replace(buffer.String(), "\n", "", -1))
		respostaCorpo = filtrarImagemCorpo(respostaCorpo)
		c.handler.Logger().Debugf("Resposta corpo: “%s”", respostaCorpo)
	}()

	return códigoHTTP
}

// filtrarImagemCorpo remove o excesso de caracteres dos campos de imagem
// para inseri-los nos logs. Estamos armazenando os primeiros e os últimos 5
// caracteres dos campos de imagem.
func filtrarImagemCorpo(corpo string) string {
	corpo = filtroImagemCorpo.ReplaceAllStringFunc(corpo, func(imagem string) string {
		imagemPartes := strings.Split(imagem, ":")
		imagemConteúdo := imagemPartes[1]
		imagemConteúdo = strings.TrimSpace(imagemConteúdo)

		if len(imagemConteúdo) > 12 {
			imagemConteúdo = imagemConteúdo[:6] + "..." + imagemConteúdo[len(imagemConteúdo)-6:]
		}

		return imagemPartes[0] + ":" + imagemConteúdo
	})

	return corpo
}
