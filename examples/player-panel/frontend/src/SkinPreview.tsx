import { useEffect, useRef } from "react";
import { IdleAnimation, SkinViewer } from "skinview3d";
import * as THREE from "three";

interface SkinPreviewProps {
  skinUrl: string;
  capeUrl?: string | null;
  elytraUrl?: string | null;
  model?: string;
}

export function SkinPreview({ skinUrl, capeUrl, elytraUrl, model }: SkinPreviewProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const viewerRef = useRef<SkinViewer | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return undefined;
    const viewer = new SkinViewer({
      canvas,
      width: canvas.clientWidth || 360,
      height: 420,
      skin: skinUrl,
      model: model === "slim" ? "slim" : "default",
      enableControls: true,
      background: 0x18120d,
      zoom: 0.86,
      animation: new IdleAnimation(),
      preserveDrawingBuffer: true
    });
    viewer.controls.enablePan = true;
    viewer.controls.screenSpacePanning = true;
    viewer.controls.mouseButtons = {
      LEFT: THREE.MOUSE.ROTATE,
      MIDDLE: THREE.MOUSE.PAN,
      RIGHT: THREE.MOUSE.PAN
    };
    viewer.controls.touches = {
      ONE: THREE.TOUCH.ROTATE,
      TWO: THREE.TOUCH.DOLLY_PAN
    };
    viewer.autoRotate = true;
    viewer.autoRotateSpeed = 0.35;
    viewer.resetEars();
    viewerRef.current = viewer;

    const resize = new ResizeObserver(([entry]) => {
      if (!entry) return;
      viewer.setSize(Math.max(260, Math.floor(entry.contentRect.width)), 420);
    });
    resize.observe(canvas);
    canvas.addEventListener("contextmenu", preventContextMenu);
    return () => {
      resize.disconnect();
      canvas.removeEventListener("contextmenu", preventContextMenu);
      viewer.dispose();
      viewerRef.current = null;
    };
  }, []);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer || !skinUrl) return;
    const loaded = viewer.loadSkin(skinUrl, { model: model === "slim" ? "slim" : "default" });
    void Promise.resolve(loaded).then(() => viewer.resetEars()).catch(() => undefined);
  }, [skinUrl, model]);

  useEffect(() => {
    const viewer = viewerRef.current;
    if (!viewer) return;
    if (elytraUrl) {
      void viewer.loadCape(elytraUrl, { backEquipment: "elytra" });
      return;
    }
    if (capeUrl) {
      void viewer.loadCape(capeUrl, { backEquipment: "cape" });
      return;
    }
    viewer.loadCape(null);
  }, [capeUrl, elytraUrl]);

  return (
    <div className="skin-preview-frame">
      <canvas ref={canvasRef} className="skin-preview-canvas" width={360} height={420} />
    </div>
  );
}

function preventContextMenu(event: MouseEvent) {
  event.preventDefault();
}
