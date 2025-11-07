import sqlite3
import os

# Caminho do arquivo SQLite
caminho_banco = os.path.expanduser("~/.hpa-watchdog/snapshots.db")

# Conecta ao banco de dados
conn = sqlite3.connect(caminho_banco)
cursor = conn.cursor()

# Lista todas as tabelas
cursor.execute("SELECT name FROM sqlite_master WHERE type='table';")
tabelas = [t[0] for t in cursor.fetchall()]
print(f"\n📋 Tabelas encontradas ({len(tabelas)}):")
for t in tabelas:
    print(" -", t)

# Percorre todas as tabelas e mostra seu conteúdo
for tabela in tabelas:
    print(f"\n=== Conteúdo da tabela: {tabela} ===")
    try:
        cursor.execute(f"PRAGMA table_info({tabela});")
        colunas = [c[1] for c in cursor.fetchall()]
        print("Colunas:", colunas)

        cursor.execute(f"SELECT * FROM {tabela};")
        linhas = cursor.fetchall()
        if not linhas:
            print("⚠️ (Sem registros)")
        else:
            for linha in linhas:
                print(linha)
    except Exception as e:
        print(f"Erro ao ler a tabela {tabela}: {e}")

# Fecha a conexão
conn.close()
