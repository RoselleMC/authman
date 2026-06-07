FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci \
  && npm install --no-fund --no-audit --no-save "@rollup/rollup-linux-x64-musl@$(node -p "require('./node_modules/rollup/package.json').version")"
COPY frontend ./frontend
RUN npm run build

FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=8080
COPY --from=build /app/frontend/dist ./frontend/dist
COPY backend ./backend
EXPOSE 8080
CMD ["node", "backend/server.mjs"]
