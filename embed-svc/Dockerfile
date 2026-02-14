FROM python:3.12-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Download model at build time (cached in image layer)
COPY download_model.py .
ENV MODEL_DIR=/model
RUN python download_model.py

# Copy service
COPY main.py .

EXPOSE 8091
CMD ["python", "main.py"]
