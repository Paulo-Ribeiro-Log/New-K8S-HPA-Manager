# Certificados CA Customizados para Health Checking

## 🔍 Diagnóstico Rápido

**NOVO**: Use o script de diagnóstico automatizado para verificar sua configuração:

```bash
./scripts/diagnose-ca-certificates.sh
```

O script verifica:
- ✅ Variável de ambiente `CUSTOM_CA_BUNDLE`
- ✅ Locais padrão de certificados no sistema
- ✅ Formato PEM dos certificados
- ✅ Conectividade TLS com endpoints
- ✅ Informações do certificado do servidor
- 📋 Recomendações personalizadas de correção

---

## 📋 Problema

Quando o Health Check tenta validar conectividade HTTPS com URLs internas da empresa (ex: `https://login-entrega.viavarejo.com.br/`), pode retornar erro:

```
tls: failed to verify certificate: x509: certificate is valid for ingress.local,
not login-entrega.viavarejo.com.br
```

**Por quê?**
- Aplicações no cluster confiam nos certificados porque estão configuradas corretamente
- Nossa aplicação (k8s-hpa-manager) roda fora do cluster e não tem os certificados CA da empresa
- Go precisa conhecer a CA raiz que emitiu o certificado

---

## ✅ Solução: Adicionar CA Certificates

### **Opção 1: Usar Variável de Ambiente** (Recomendado)

```bash
# Exportar caminho do arquivo CA bundle
export CUSTOM_CA_BUNDLE=/path/to/ca-certificates.crt

# Iniciar aplicação
./build/new-k8s-hpa web
```

---

### **Opção 2: Instalar no Sistema**

#### **Debian/Ubuntu**

```bash
# 1. Copiar certificado CA para diretório do sistema
sudo cp viavarejo-ca.crt /usr/local/share/ca-certificates/

# 2. Atualizar bundle de certificados
sudo update-ca-certificates

# Aplicação vai usar automaticamente: /etc/ssl/certs/ca-certificates.crt
```

#### **RHEL/CentOS/Fedora**

```bash
# 1. Copiar certificado CA
sudo cp viavarejo-ca.crt /etc/pki/ca-trust/source/anchors/

# 2. Atualizar bundle
sudo update-ca-trust extract

# Aplicação vai usar automaticamente: /etc/pki/tls/certs/ca-bundle.crt
```

#### **macOS**

```bash
# Adicionar ao Keychain do sistema
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain viavarejo-ca.crt
```

---

### **Opção 3: Arquivo Local (Desenvolvimento)**

```bash
# Copiar certificado para diretório da aplicação
cp viavarejo-ca.crt ./ca-certificates.crt

# Aplicação detecta automaticamente
./build/new-k8s-hpa web
```

---

## 🔍 Como Obter o Certificado CA

### **Método 1: Exportar do Navegador**

1. Acesse `https://login-entrega.viavarejo.com.br/` no navegador
2. Clique no **cadeado** → **Certificado**
3. Vá para a aba **Caminho de Certificação**
4. Selecione o certificado **raiz** (topo da hierarquia)
5. **Exportar** como `.crt` ou `.pem`

### **Método 2: OpenSSL**

```bash
# Obter certificados do servidor
openssl s_client -showcerts -connect login-entrega.viavarejo.com.br:443 \
  </dev/null 2>/dev/null | \
  openssl x509 -outform PEM > viavarejo-ca.crt
```

### **Método 3: Buscar Certificado Interno**

Se a empresa usa **CA privada**, peça ao time de infra o arquivo `.crt` ou `.pem` do certificado raiz.

---

## 🧪 Testar Configuração

```bash
# Verificar se certificado está no bundle
grep "BEGIN CERTIFICATE" /etc/ssl/certs/ca-certificates.crt

# Testar conexão manualmente (curl usa mesmo pool de CAs)
curl https://login-entrega.viavarejo.com.br/

# Executar Health Check e verificar logs
./build/new-k8s-hpa web
# Deve aparecer: "Loaded custom CA certificates" nos logs
```

---

## 📊 Locais Verificados pela Aplicação

A aplicação busca certificados automaticamente nesta ordem:

1. **Variável de ambiente**: `$CUSTOM_CA_BUNDLE`
2. **Debian/Ubuntu**: `/etc/ssl/certs/ca-certificates.crt`
3. **RHEL/CentOS**: `/etc/pki/tls/certs/ca-bundle.crt`
4. **OpenSUSE**: `/etc/ssl/ca-bundle.pem`
5. **Manual install**: `/usr/local/share/ca-certificates/ca-bundle.crt`
6. **Local (dev)**: `./ca-certificates.crt`

Se encontrar o arquivo, adiciona ao pool de CAs confiáveis automaticamente.

---

## ⚠️ Troubleshooting

### **Erro persiste após adicionar certificado**

```bash
# Verificar se certificado foi adicionado
openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt viavarejo-ca.crt

# Verificar formato do certificado (deve ser PEM)
openssl x509 -in viavarejo-ca.crt -text -noout
```

### **"No valid certificates found in CA bundle"**

- Certificado está no formato errado (deve ser PEM, não DER)
- Arquivo corrompido ou incompleto

```bash
# Converter DER para PEM
openssl x509 -inform DER -in cert.der -out cert.pem
```

### **Logs não mostram "Loaded custom CA certificates"**

- Arquivo não foi encontrado em nenhum local padrão
- Use variável de ambiente `CUSTOM_CA_BUNDLE` explicitamente

---

## 📝 Exemplo Completo

```bash
# 1. Obter certificado CA da empresa
openssl s_client -showcerts -connect login-entrega.viavarejo.com.br:443 \
  </dev/null 2>/dev/null | \
  sed -n '/BEGIN CERTIFICATE/,/END CERTIFICATE/p' > viavarejo-ca.crt

# 2. Instalar no sistema
sudo cp viavarejo-ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates

# 3. Reiniciar servidor web
new-k8s-hpa-web restart

# 4. Verificar logs
tail -f /tmp/new-k8s-hpa-web.log | grep "Loaded custom CA"
```

---

## 🔒 Segurança

- ✅ **Não usar `InsecureSkipVerify`** - sempre validar certificados
- ✅ **Adicionar apenas CAs confiáveis** - não adicionar certificados de servidores individuais
- ✅ **Manter certificados atualizados** - renovar quando expirarem

---

**Última atualização**: 29/12/2025
