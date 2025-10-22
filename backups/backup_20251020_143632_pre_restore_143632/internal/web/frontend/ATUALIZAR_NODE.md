# ⚠️ Atualização Necessária do Node.js

## Problema Atual

A interface web React requer **Node.js >= 18**, mas você tem **Node.js 12.22.9** (muito antigo).

```bash
$ node --version
v12.22.9  # ❌ Muito antigo (2019)

# Vite requer:
Node >= 18.0.0  # ✅ Necessário
```

## 📋 Opções de Solução

### Opção 1: Usar NVM (Recomendado) ⭐

NVM permite ter múltiplas versões do Node sem conflitos:

```bash
# 1. Instalar NVM
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash

# 2. Recarregar shell
source ~/.bashrc

# 3. Instalar Node.js LTS
nvm install --lts
nvm use --lts

# 4. Verificar
node --version  # Deve mostrar v20.x.x ou v22.x.x

# 5. Build frontend
cd /home/paulo/scripts/Scripts-GO/Scale_HPA
make web-build
```

### Opção 2: Atualizar Node via Package Manager

#### Ubuntu/Debian
```bash
# Adicionar repositório NodeSource
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -

# Instalar Node 20 LTS
sudo apt-get install -y nodejs

# Verificar
node --version
npm --version
```

#### WSL/Ubuntu (sua situação)
```bash
# Via NodeSource
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt-get install -y nodejs

# Ou via NVM (preferível)
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash
source ~/.bashrc
nvm install 20
nvm use 20
```

### Opção 3: Build em Outra Máquina

Se não puder atualizar agora:

```bash
# Em uma máquina com Node >= 18
cd internal/web/frontend
npm install
npm run build

# Copiar pasta static/ gerada para sua máquina
# Então apenas compile o Go:
make build
```

## 🚀 Após Atualizar

```bash
# 1. Build frontend
cd /home/paulo/scripts/Scripts-GO/Scale_HPA
make web-build

# 2. Build backend (com frontend embedado)
make build

# 3. Executar
./build/k8s-hpa-manager web --port 8080

# 4. Acessar
# http://localhost:8080
```

## ✅ Verificação

Após instalar Node >= 18:

```bash
node --version   # Deve ser >= 18
npm --version    # Deve ser >= 8

# Testar build
cd /home/paulo/scripts/Scripts-GO/Scale_HPA
make web-build
```

## 📚 Mais Informações

- **NVM**: https://github.com/nvm-sh/nvm
- **NodeSource**: https://github.com/nodesource/distributions
- **Node.js Downloads**: https://nodejs.org/
- **Vite Requirements**: https://vitejs.dev/guide/#scaffolding-your-first-vite-project

---

**Recomendação:** Use NVM! É mais fácil gerenciar versões e não conflita com pacotes do sistema.
