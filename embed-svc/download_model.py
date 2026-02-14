"""
Download all-MiniLM-L6-v2 ONNX model and tokenizer from HuggingFace.
Run once during Docker build to bundle model into image.
"""

import os
import urllib.request
import json

MODEL_DIR = os.environ.get("MODEL_DIR", "/model")
os.makedirs(MODEL_DIR, exist_ok=True)

BASE_URL = "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main"

FILES = {
    "model.onnx": f"{BASE_URL}/onnx/model.onnx",
    "tokenizer.json": f"{BASE_URL}/tokenizer.json",
}

for filename, url in FILES.items():
    dest = os.path.join(MODEL_DIR, filename)
    if os.path.exists(dest):
        print(f"Already exists: {dest}")
        continue
    print(f"Downloading {filename} from {url}")
    urllib.request.urlretrieve(url, dest)
    size_mb = os.path.getsize(dest) / (1024 * 1024)
    print(f"  Saved: {dest} ({size_mb:.1f} MB)")

print("Model download complete")
