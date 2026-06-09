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
    const viewer = new SkinViewer({
      canvas,
      width: canvas.clientWidth || 360,
      height: canvas.clientHeight || 420,
      skin: assetURL(skinUrl),
      model: model === "slim" ? "slim" : "default",
      enableControls: true,
      background: 0xf7fbf7,
      zoom: 0.86,
      nameTag: name,
      animation: new IdleAnimation(),
      preserveDrawingBuffer: true,
    });
    viewer.globalLight.intensity = 3;
    viewer.cameraLight.intensity = 0.6;
    viewer.renderer.setClearColor(0xf7fbf7, 1);
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
    return () => {
      resize.disconnect();
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
