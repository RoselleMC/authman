import { useEffect, useRef } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import type { LimboBlueprintPreview as PreviewData, LimboBlueprintPreviewBlock } from "../api/admin";

export interface SpawnPoint {
  x: number;
  y: number;
  z: number;
  yaw: number;
  pitch: number;
}

interface Props {
  preview: PreviewData | undefined;
  spawn?: SpawnPoint;
  onSpawnChange?: (spawn: SpawnPoint) => void;
}

function blockColor(name: string): number {
  if (name.includes("grass")) return 0x4f8f3a;
  if (name.includes("leaves")) return 0x2f6f3e;
  if (name.includes("wood") || name.includes("log") || name.includes("planks")) return 0x8b5a2b;
  if (name.includes("water")) return 0x3b82f6;
  if (name.includes("lava")) return 0xef4444;
  if (name.includes("sand")) return 0xd8c26a;
  if (name.includes("stone") || name.includes("cobble")) return 0x7c8188;
  if (name.includes("glass")) return 0x93c5fd;
  if (name.includes("bedrock")) return 0x30343b;
  return 0x9ca3af;
}

function writeBoxEdges(target: Float32Array, offset: number, x: number, y: number, z: number) {
  const x0 = x - 0.5;
  const x1 = x + 0.5;
  const y0 = y - 0.5;
  const y1 = y + 0.5;
  const z0 = z - 0.5;
  const z1 = z + 0.5;
  function edge(ax: number, ay: number, az: number, bx: number, by: number, bz: number) {
    target[offset++] = ax;
    target[offset++] = ay;
    target[offset++] = az;
    target[offset++] = bx;
    target[offset++] = by;
    target[offset++] = bz;
  }
  edge(x0, y0, z0, x1, y0, z0);
  edge(x1, y0, z0, x1, y0, z1);
  edge(x1, y0, z1, x0, y0, z1);
  edge(x0, y0, z1, x0, y0, z0);
  edge(x0, y1, z0, x1, y1, z0);
  edge(x1, y1, z0, x1, y1, z1);
  edge(x1, y1, z1, x0, y1, z1);
  edge(x0, y1, z1, x0, y1, z0);
  edge(x0, y0, z0, x0, y1, z0);
  edge(x1, y0, z0, x1, y1, z0);
  edge(x1, y0, z1, x1, y1, z1);
  edge(x0, y0, z1, x0, y1, z1);
  return offset;
}

export function LimboBlueprintPreview({ preview, spawn, onSpawnChange }: Props) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const spawnRef = useRef<SpawnPoint | undefined>(spawn);
  const markerRef = useRef<THREE.Mesh | null>(null);
  const centerRef = useRef<THREE.Vector3>(new THREE.Vector3());
  const renderRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    spawnRef.current = spawn;
    const marker = markerRef.current;
    if (marker && spawn) {
      const center = centerRef.current;
      marker.position.set(spawn.x - center.x, spawn.y + 0.9 - center.y, spawn.z - center.z);
      marker.visible = true;
      renderRef.current?.();
    }
  }, [spawn]);

  useEffect(() => {
    const root = rootRef.current;
    const blocks = preview?.blocks ?? [];
    if (!root || blocks.length === 0) return undefined;

    const width = Math.max(320, root.clientWidth || 640);
    const height = Math.max(320, root.clientHeight || 420);
    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, preserveDrawingBuffer: true });
    renderer.setSize(width, height);
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    renderer.domElement.style.touchAction = "none";
    renderer.domElement.style.userSelect = "none";
    renderer.domElement.draggable = false;
    root.innerHTML = "";
    root.appendChild(renderer.domElement);

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(45, width / height, 0.1, 5000);
    scene.add(new THREE.AmbientLight(0xffffff, 0.72));
    const light = new THREE.DirectionalLight(0xffffff, 0.9);
    light.position.set(40, 80, 20);
    scene.add(light);

    const group = new THREE.Group();
    scene.add(group);
    const geometry = new THREE.BoxGeometry(1, 1, 1);
    const edgesMaterial = new THREE.LineBasicMaterial({
      color: 0x111827,
      transparent: true,
      opacity: 0.34,
      depthTest: true,
      depthWrite: false,
    });
    const materialCache = new Map<number, THREE.MeshLambertMaterial>();
    const paletteNames = new Map((preview?.palette ?? []).map((entry) => [entry.id, entry.name]));
    const blockGroups = new Map<number, LimboBlueprintPreviewBlock[]>();
    const bounds = preview?.bounds;
    const center = new THREE.Vector3(
      bounds ? bounds.min_x + bounds.width / 2 : 0,
      bounds ? bounds.min_y + bounds.height / 2 : 0,
      bounds ? bounds.min_z + bounds.length / 2 : 0,
    );
    centerRef.current = center;
    const size = Math.max(bounds?.width ?? 16, bounds?.height ?? 16, bounds?.length ?? 16, 16);
    const distance = size * 2.4;
    camera.position.set(distance, distance * 0.82, distance);
    camera.near = Math.max(0.1, distance / 100);
    camera.far = Math.max(5000, distance * 12);
    camera.lookAt(0, 0, 0);
    camera.updateProjectionMatrix();

    for (const block of blocks) {
      const color = blockColor(block.name ?? paletteNames.get(block.p) ?? "");
      const groupBlocks = blockGroups.get(color);
      if (groupBlocks) groupBlocks.push(block);
      else blockGroups.set(color, [block]);
    }

    const pickMeshes: THREE.InstancedMesh[] = [];
    const transform = new THREE.Matrix4();
    for (const [color, groupBlocks] of blockGroups) {
      let material = materialCache.get(color);
      if (!material) {
        material = new THREE.MeshLambertMaterial({ color });
        materialCache.set(color, material);
      }
      const mesh = new THREE.InstancedMesh(geometry, material, groupBlocks.length);
      mesh.userData.blocks = groupBlocks;
      for (let index = 0; index < groupBlocks.length; index++) {
        const block = groupBlocks[index];
        if (!block) continue;
        transform.makeTranslation(block.x - center.x, block.y - center.y, block.z - center.z);
        mesh.setMatrixAt(index, transform);
      }
      mesh.instanceMatrix.needsUpdate = true;
      group.add(mesh);
      pickMeshes.push(mesh);
    }

    const edgesGeometry = new THREE.BufferGeometry();
    const edgePositions = new Float32Array(blocks.length * 72);
    let edgeOffset = 0;
    for (const block of blocks) {
      edgeOffset = writeBoxEdges(edgePositions, edgeOffset, block.x - center.x, block.y - center.y, block.z - center.z);
    }
    edgesGeometry.setAttribute("position", new THREE.BufferAttribute(edgePositions, 3));
    const edges = new THREE.LineSegments(edgesGeometry, edgesMaterial);
    group.add(edges);

    const marker = new THREE.Mesh(
      new THREE.ConeGeometry(0.45, 1.4, 24),
      new THREE.MeshBasicMaterial({ color: 0xef4444 }),
    );
    marker.rotation.x = Math.PI;
    marker.visible = !!spawnRef.current;
    if (spawnRef.current) {
      marker.position.set(spawnRef.current.x - center.x, spawnRef.current.y + 0.9 - center.y, spawnRef.current.z - center.z);
    }
    scene.add(marker);
    markerRef.current = marker;

    const raycaster = new THREE.Raycaster();
    const pointer = new THREE.Vector2();
    const controls = new OrbitControls(camera, renderer.domElement);
    controls.target.set(0, 0, 0);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.enablePan = true;
    controls.enableZoom = true;
    controls.minDistance = Math.max(2, size * 0.25);
    controls.maxDistance = Math.max(60, size * 8);
    controls.maxPolarAngle = Math.PI;
    controls.screenSpacePanning = true;
    controls.mouseButtons = {
      LEFT: THREE.MOUSE.ROTATE,
      MIDDLE: THREE.MOUSE.PAN,
      RIGHT: THREE.MOUSE.PAN,
    };
    controls.touches = {
      ONE: THREE.TOUCH.ROTATE,
      TWO: THREE.TOUCH.DOLLY_PAN,
    };
    controls.update();

    let pointerDown: { x: number; y: number; id: number; panGesture: boolean } | null = null;

    function render() {
      renderer.render(scene, camera);
    }
    renderRef.current = render;
    function pointerCoords(event: PointerEvent) {
      const rect = renderer.domElement.getBoundingClientRect();
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -(((event.clientY - rect.top) / rect.height) * 2 - 1);
    }
    function onPointerDown(event: PointerEvent) {
      pointerDown = {
        x: event.clientX,
        y: event.clientY,
        id: event.pointerId,
        panGesture: event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey,
      };
    }
    function onPointerUp(event: PointerEvent) {
      const start = pointerDown;
      pointerDown = null;
      if (!start || start.id !== event.pointerId || !onSpawnChange) return;
      if (start.panGesture) return;
      const moved = Math.hypot(event.clientX - start.x, event.clientY - start.y) > 4;
      if (moved) return;
      pointerCoords(event);
      raycaster.setFromCamera(pointer, camera);
      const hits = raycaster.intersectObjects(pickMeshes, false);
      const hit = hits.find((item) => typeof item.instanceId === "number");
      if (!hit) return;
      const meshBlocks = hit.object.userData.blocks as LimboBlueprintPreviewBlock[] | undefined;
      const block = meshBlocks?.[hit.instanceId ?? -1];
      if (!block) return;
      const current = spawnRef.current;
      const next = { x: block.x + 0.5, y: block.y + 1, z: block.z + 0.5, yaw: current?.yaw ?? 0, pitch: current?.pitch ?? 0 };
      marker.position.set(next.x - center.x, next.y + 0.9 - center.y, next.z - center.z);
      marker.visible = true;
      spawnRef.current = next;
      onSpawnChange(next);
      render();
    }
    function onContextMenu(event: MouseEvent) {
      event.preventDefault();
    }
    function animate() {
      controls.update();
      render();
    }
    const container = root;
    function onResize() {
      const nextWidth = Math.max(320, container.clientWidth || 640);
      const nextHeight = Math.max(320, container.clientHeight || 420);
      renderer.setSize(nextWidth, nextHeight);
      camera.aspect = nextWidth / nextHeight;
      camera.updateProjectionMatrix();
      render();
    }
    const resize = new ResizeObserver(onResize);
    resize.observe(root);

    renderer.domElement.addEventListener("pointerdown", onPointerDown);
    renderer.domElement.addEventListener("pointerup", onPointerUp);
    renderer.domElement.addEventListener("contextmenu", onContextMenu);
    renderer.setAnimationLoop(animate);
    render();

    return () => {
      renderRef.current = null;
      markerRef.current = null;
      resize.disconnect();
      renderer.domElement.removeEventListener("pointerdown", onPointerDown);
      renderer.domElement.removeEventListener("pointerup", onPointerUp);
      renderer.domElement.removeEventListener("contextmenu", onContextMenu);
      renderer.setAnimationLoop(null);
      controls.dispose();
      marker.geometry.dispose();
      const markerMaterial = marker.material;
      if (Array.isArray(markerMaterial)) markerMaterial.forEach((m) => m.dispose());
      else markerMaterial.dispose();
      edgesMaterial.dispose();
      edgesGeometry.dispose();
      geometry.dispose();
      for (const material of materialCache.values()) material.dispose();
      renderer.dispose();
      root.innerHTML = "";
    };
  }, [preview, onSpawnChange]);

  if (!preview?.blocks?.length) {
    return <div className="blueprint-preview blueprint-preview--empty">No schematic preview</div>;
  }
  return <div ref={rootRef} className="blueprint-preview" data-testid="blueprint-3d-preview" />;
}
