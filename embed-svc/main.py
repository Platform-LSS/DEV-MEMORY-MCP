"""
Lightweight embedding service using all-MiniLM-L6-v2 via ONNX Runtime.
No PyTorch dependency — uses onnxruntime + tokenizers only (~200MB total).
Exposes POST /embed endpoint returning 384-dim vectors.
"""

import os
import json
import logging
import numpy as np
from http.server import HTTPServer, BaseHTTPRequestHandler
from onnxruntime import InferenceSession
from tokenizers import Tokenizer

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger(__name__)

MODEL_DIR = os.environ.get("MODEL_DIR", "/model")
PORT = int(os.environ.get("PORT", "8091"))
MAX_LENGTH = int(os.environ.get("MAX_LENGTH", "128"))


def load_model():
    """Load ONNX model and tokenizer."""
    model_path = os.path.join(MODEL_DIR, "model.onnx")
    tokenizer_path = os.path.join(MODEL_DIR, "tokenizer.json")

    logger.info(f"Loading model from {model_path}")
    session = InferenceSession(model_path, providers=["CPUExecutionProvider"])

    logger.info(f"Loading tokenizer from {tokenizer_path}")
    tokenizer = Tokenizer.from_file(tokenizer_path)
    tokenizer.enable_truncation(max_length=MAX_LENGTH)
    tokenizer.enable_padding(length=MAX_LENGTH)

    logger.info("Model and tokenizer loaded successfully")
    return session, tokenizer


def mean_pooling(token_embeddings, attention_mask):
    """Mean pooling with attention mask."""
    mask_expanded = np.expand_dims(attention_mask, axis=-1)
    sum_embeddings = np.sum(token_embeddings * mask_expanded, axis=1)
    sum_mask = np.clip(np.sum(mask_expanded, axis=1), a_min=1e-9, a_max=None)
    return sum_embeddings / sum_mask


def normalize(embeddings):
    """L2 normalize embeddings."""
    norms = np.linalg.norm(embeddings, axis=1, keepdims=True)
    return embeddings / np.clip(norms, a_min=1e-9, a_max=None)


# Load model at startup
session, tokenizer = load_model()


class EmbedHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/embed":
            self.send_error(404)
            return

        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length)

        try:
            data = json.loads(body)
            text = data.get("text", "")
            if not text:
                self.send_error(400, "missing 'text' field")
                return

            # Tokenize
            encoded = tokenizer.encode(text)
            input_ids = np.array([encoded.ids], dtype=np.int64)
            attention_mask = np.array([encoded.attention_mask], dtype=np.int64)
            token_type_ids = np.zeros_like(input_ids, dtype=np.int64)

            # Run inference
            outputs = session.run(
                None,
                {
                    "input_ids": input_ids,
                    "attention_mask": attention_mask,
                    "token_type_ids": token_type_ids,
                },
            )

            # Mean pooling + normalize
            token_embeddings = outputs[0]  # (1, seq_len, 384)
            pooled = mean_pooling(token_embeddings, attention_mask)
            normalized = normalize(pooled)
            embedding = normalized[0].tolist()

            # Respond
            response = json.dumps({"embedding": embedding})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(response.encode())

        except Exception as e:
            logger.error(f"Embedding error: {e}")
            self.send_error(500, str(e))

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"status": "ok", "model": "all-MiniLM-L6-v2", "dim": 384}).encode())
            return
        self.send_error(404)

    def log_message(self, format, *args):
        # Suppress default access logs, use logger instead
        pass


if __name__ == "__main__":
    server = HTTPServer(("0.0.0.0", PORT), EmbedHandler)
    logger.info(f"Embedding service listening on port {PORT}")
    logger.info(f"Model: all-MiniLM-L6-v2, dim: 384, max_length: {MAX_LENGTH}")
    server.serve_forever()
