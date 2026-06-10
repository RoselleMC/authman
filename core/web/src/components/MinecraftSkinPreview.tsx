import { useEffect, useRef } from "react";
import { SkinViewer, IdleAnimation } from "skinview3d";
import * as THREE from "three";
import { getRuntimeConfig } from "@authman/shared";

interface MinecraftSkinPreviewProps {
  skinUrl: string;
  capeUrl?: string | null;
  elytraUrl?: string | null;
  model?: string;
  name?: string;
}

export function MinecraftSkinPreview({ skinUrl, capeUrl, elytraUrl, model, name }: MinecraftSkinPreviewProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const viewerRef = useRef<SkinViewer | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return undefined;
    const theme = readSkinPreviewTheme(canvas);
    const viewer = new SkinViewer({
      canvas,
      width: canvas.clientWidth || 360,
      height: canvas.clientHeight || 420,
      skin: assetURL(skinUrl),
      model: model === "slim" ? "slim" : "default",
      enableControls: true,
      background: theme.background,
      zoom: 0.86,
      nameTag: name,
      animation: new IdleAnimation(),
      preserveDrawingBuffer: true,
    });
    applySkinPreviewTheme(viewer, canvas);
    viewer.resetEars();
    viewer.controls.enablePan = true;
    viewer.controls.screenSpacePanning = true;
    viewer.controls.mouseButtons = {
      LEFT: THREE.MOUSE.ROTATE,
      MIDDLE: THREE.MOUSE.PAN,
      RIGHT: THREE.MOUSE.PAN,
    };
    viewer.controls.touches = {
      ONE: THREE.TOUCH.ROTATE,
      TWO: THREE.TOUCH.DOLLY_PAN,
    };
    canvas.addEventListener("contextmenu", preventContextMenu);
    viewer.autoRotate = true;
    viewer.autoRotateSpeed = 0.35;
    viewerRef.current = viewer;
    const resize = new ResizeObserver(([entry]) => {
      if (!entry) return;
      const width = Math.max(260, Math.floor(entry.contentRect.width));
      viewer.setSize(width, 420);
    });
    resize.observe(canvas);
    const themeObserver = new MutationObserver(() => applySkinPreviewTheme(viewer, canvas));
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => {
      resize.disconnect();
      themeObserver.disconnect();
      canvas.removeEventListener("contextmenu", preventContextMenu);
      viewer.dispose();
      viewerRef.current = null;
    };
  }, [skinUrl, model, name]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer) return;
    const loaded = viewer.loadSkin(assetURL(skinUrl), { model: model === "slim" ? "slim" : "default" });
    viewer.resetEars();
    void Promise.resolve(loaded).then(() => viewer.resetEars()).catch(() => undefined);
  }, [skinUrl, model]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer) return;
    if (elytraUrl) {
      void viewer.loadCape(assetURL(elytraUrl), { backEquipment: "elytra" });
      return;
    }
    if (capeUrl) {
      void viewer.loadCape(assetURL(capeUrl), { backEquipment: "cape" });
      return;
    }
    viewer.loadCape(null);
  }, [capeUrl, elytraUrl]);

  return (
    <div className="skin-preview-frame" data-testid="skin-preview">
      <canvas ref={canvasRef} className="skin-preview-canvas" width={360} height={420} />
    </div>
  );
}

function preventContextMenu(event: MouseEvent) {
  event.preventDefault();
}

function assetURL(url: string | null | undefined) {
  if (!url) return "";
  if (url.startsWith("http://") || url.startsWith("https://") || url.startsWith("data:")) return url;
  const cfg = getRuntimeConfig();
  if (url.startsWith("/api/") && cfg.apiBase.endsWith("/api")) {
    return `${cfg.apiBase}${url.slice(4)}`;
  }
  return url;
}

function applySkinPreviewTheme(viewer: SkinViewer, canvas: HTMLCanvasElement) {
  const theme = readSkinPreviewTheme(canvas);
  viewer.background = theme.background;
  viewer.renderer.setClearColor(theme.background, 1);
  viewer.globalLight.intensity = theme.dark ? 3.4 : 3;
  viewer.cameraLight.intensity = theme.dark ? 1.1 : 0.6;
  viewer.render();
}

function readSkinPreviewTheme(canvas: HTMLCanvasElement) {
  const frame = canvas.closest(".skin-preview-frame") as HTMLElement | null;
  const style = getComputedStyle(frame ?? canvas);
  const dark = document.documentElement.getAttribute("data-theme") === "dark";
  return {
    background: cssColorToHex(style.getPropertyValue("--skin-preview-canvas-bg").trim(), dark ? 0x111815 : 0xf4f8f4),
    dark,
  };
}

function cssColorToHex(color: string, fallback: number) {
  if (!color) return fallback;
  try {
    return new THREE.Color(color).getHex();
  } catch {
    // Some browsers normalize custom properties through canvas even when Three.js
    // cannot parse the exact color syntax.
  }
  const ctx = document.createElement("canvas").getContext("2d");
  if (!ctx) return fallback;
  ctx.fillStyle = "#000";
  ctx.fillStyle = color;
  const normalized = ctx.fillStyle;
  if (/^#[0-9a-f]{6}$/i.test(normalized)) {
    return Number.parseInt(normalized.slice(1), 16);
  }
  return fallback;
}
