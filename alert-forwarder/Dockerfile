FROM python:3.14-slim@sha256:bc389f7dfcb21413e72a28f491985326994795e34d2b86c8ae2f417b4e7818aa

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
