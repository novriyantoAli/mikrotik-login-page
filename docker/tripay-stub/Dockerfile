# Tripay stub for the end-to-end smoke journey (ADR 0018 §5). No dependencies
# beyond Node's own http/crypto, so the image costs one layer and boots instantly.
FROM node:22-alpine

WORKDIR /app

COPY server.mjs ./

USER node

EXPOSE 8080

CMD ["node", "server.mjs"]
