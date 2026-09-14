"""Hardware-free lifecycle and Super I/O probe regressions."""
import os
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix="tad-lifecycle-") as directory:
    root = Path(directory)
    (root / "main").write_text((repo / "cmd/main").read_text())
    (root / "it87_dkms.sh").write_text("it87_ensure() { return 0; }\n")
    (root / "bin").mkdir()
    backend = root / "bin/tad-module"
    backend.write_text('#!/bin/sh\nprintf started > "$TRIM_PKGVAR/started"\nexit 1\n')
    backend.chmod(0o755)
    env = dict(os.environ, TRIM_APPDEST=directory, TRIM_PKGETC=directory, TRIM_PKGVAR=directory)
    subprocess.run(["sh", str(root / "main"), "restart"], env=env, capture_output=True, timeout=5)
    assert (root / "started").exists(), "restart did not invoke backend"

    # Compile the shipped probe with mocked I/O, never executing port access.
    source = (repo / "cmd/it87_dkms.sh").read_text().split("<<'CEOF'\n", 1)[1].split("\nCEOF", 1)[0]
    source = source.replace("#include <sys/io.h>", """
static unsigned int reg, step;
static void outb(unsigned int v, unsigned int p) {
    if (p != 0x4e) return;
    if (step < 4) {
        const unsigned int unlock[] = {0x87, 0x01, 0x55, 0xaa};
        if (v == unlock[step]) step++; else step = 0;
    } else reg = v;
}
static unsigned char inb(unsigned int p) {
    if (p != 0x4f || step != 4) return 0xff;
    return reg == 0x20 ? 0x86 : reg == 0x21 ? 0x13 : 0xff;
}
static int ioperm(unsigned int p, unsigned int n, int on) { return p == 0x4e ? 0 : -1; }
""")
    (root / "probe.c").write_text(source)
    subprocess.run(["cc", str(root / "probe.c"), "-o", str(root / "probe")], check=True)
    result = subprocess.run([str(root / "probe")], check=True, capture_output=True, text=True)
    assert result.stdout.strip() == "0x8613", result.stdout
print("PASS: restart and Super I/O byte order / 0x4e unlock")
