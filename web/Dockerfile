FROM node:24-alpine
RUN apk add --no-cache wget
WORKDIR /app
COPY package.json ./
COPY src/ ./src/
EXPOSE 5390
CMD ["node", "src/server.mjs"]
