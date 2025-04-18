FROM python:3.13-slim@sha256:21e39cf1815802d4c6f89a0d3a166cc67ce58f95b6d1639e68a394c99310d2e5

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY ./forwarder/*.py ./forwarder/

# hide most server logs by default so that Koney trap alerts
# can be seen more easily (they are logged regardless)
ENV UVICORN_LOG_LEVEL=error

USER 65532:65532

EXPOSE 8000

ENTRYPOINT ["uvicorn"]
CMD ["forwarder.main:app", "--host", "0.0.0.0", "--port", "8000"]
