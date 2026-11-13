import { describe, expect, it } from "vitest";
import { fmtDurationMs } from "./fmtDuration";

describe("fmtDurationMs", () => {
  it("microssegundos abaixo de 1 ms", () => {
    expect(fmtDurationMs(0.03)).toBe("30 µs");
    expect(fmtDurationMs(0.5)).toBe("500 µs");
    expect(fmtDurationMs(0.0004)).toBe("1 µs"); // nunca "0 µs"
    expect(fmtDurationMs(0.9996)).toBe("1 ms"); // arredonda para cima sem virar "1000 µs"
  });
  it("milissegundos: uma casa abaixo de 10 e inteiros até 1 s", () => {
    expect(fmtDurationMs(0)).toBe("0 ms");
    expect(fmtDurationMs(1)).toBe("1 ms");
    expect(fmtDurationMs(4.25)).toBe("4,3 ms");
    expect(fmtDurationMs(73)).toBe("73 ms");
    expect(fmtDurationMs(268)).toBe("268 ms");
    expect(fmtDurationMs(220.4)).toBe("220 ms");
  });
  it("segundos a partir de 1000 ms, com vírgula e casas que diminuem com o tamanho", () => {
    expect(fmtDurationMs(1000)).toBe("1 s");
    expect(fmtDurationMs(1300)).toBe("1,3 s");
    expect(fmtDurationMs(1501.3)).toBe("1,5 s");
    expect(fmtDurationMs(1234)).toBe("1,23 s");
    expect(fmtDurationMs(9990)).toBe("9,99 s");
    expect(fmtDurationMs(42000)).toBe("42 s");
    expect(fmtDurationMs(42500)).toBe("42,5 s");
    expect(fmtDurationMs(125000)).toBe("125 s");
  });
  it("na fronteira de 1 s não aparece 1000 ms", () => {
    expect(fmtDurationMs(999.6)).toBe("1 s");
    expect(fmtDurationMs(999.4)).toBe("999 ms");
  });
  it("valor inválido ou negativo devolve vazio", () => {
    expect(fmtDurationMs(NaN)).toBe("");
    expect(fmtDurationMs(Infinity)).toBe("");
    expect(fmtDurationMs(-5)).toBe("");
  });
});
