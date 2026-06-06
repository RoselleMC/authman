# syntax=docker/dockerfile:1.7
FROM node:20-alpine AS build
WORKDIR /workspace

COPY package.json package-lock.json* .npmrc tsconfig.base.json ./
COPY shared/package.json shared/tsconfig.json shared/
COPY player/package.json player/tsconfig.json player/vite.config.ts player/env.d.ts player/index.html player/
COPY admin/package.json admin/
COPY e2e/package.json e2e/

RUN npm install --no-fund --no-audit

COPY shared/src ./shared/src
COPY player/src ./player/src

RUN npm run build -w @authman/player

FROM nginx:1.27-alpine AS runtime

RUN rm /etc/nginx/conf.d/default.conf
COPY player/nginx.conf /etc/nginx/conf.d/default.conf

COPY --from=build /workspace/player/dist /usr/share/nginx/html

COPY player/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080
ENV AUTHMAN_API_BASE=/api \
    AUTHMAN_DEFAULT_LOCALE=en

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
