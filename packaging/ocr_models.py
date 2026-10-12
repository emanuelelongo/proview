"""Fetch the two pinned development OCR models, checking their SHA-256 hashes."""
import hashlib
from pathlib import Path
import urllib.request

REVISION = "87416418657359cb625c412a48b6e1d6d41c29bd"
MODELS = {
    "eng": "7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2",
    "ita": "b8f89e1e785118dac4d51ae042c029a64edb5c3ee42ef73027a6d412748d8827",
}


def main(target=None):
    target = target or Path(__file__).resolve().parent.parent / "build" / "ocr" / "tessdata"
    target.mkdir(parents=True, exist_ok=True)
    for language, digest in MODELS.items():
        path = target / f"{language}.traineddata"
        if path.exists() and hashlib.sha256(path.read_bytes()).hexdigest() == digest:
            continue
        url = f"https://raw.githubusercontent.com/tesseract-ocr/tessdata_fast/{REVISION}/{path.name}"
        with urllib.request.urlopen(url, timeout=60) as response:
            data = response.read()
        if hashlib.sha256(data).hexdigest() != digest:
            raise RuntimeError(f"Invalid checksum for {path.name}")
        temporary = path.with_suffix(".download")
        temporary.write_bytes(data)
        temporary.replace(path)
    print(f"OCR models ready: {target}")


if __name__ == "__main__":
    main()
