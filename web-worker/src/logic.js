import { SIDEBAR_HTML } from './templates.js';

// === 1. API 处理函数 (搜索/随机) ===
export async function handleApiPosts(url, env) {
  const q = url.searchParams.get('q');
  const offset = url.searchParams.get('offset') || 0;

  // 随机图逻辑
  if (q === 'random') {
    const { results } = await env.DB.prepare("SELECT * FROM images ORDER BY RANDOM() LIMIT 1").all();
    return new Response(JSON.stringify(results), { headers: { 'Content-Type': 'application/json' }});
  }

  // 搜索构建逻辑
  let sql;
  let params = [];
  if (q) {
    const keywords = q.replace(/#/g, '').trim().split(/\s+/).filter(k => k.length > 0);
    if (keywords.length > 0) {
      const conditions = keywords.map(() => `(tags LIKE ? OR caption LIKE ?)`).join(' AND ');
      sql = `SELECT * FROM images WHERE ${conditions} ORDER BY created_at DESC LIMIT 20 OFFSET ?`;
      keywords.forEach(k => { params.push(`%${k}%`); params.push(`%${k}%`); });
      params.push(offset);
    } else {
      sql = `SELECT * FROM images ORDER BY created_at DESC LIMIT 20 OFFSET ?`;
      params = [offset];
    }
  } else {
    sql = `SELECT * FROM images ORDER BY created_at DESC LIMIT 20 OFFSET ?`;
    params = [offset];
  }

  try {
    const { results } = await env.DB.prepare(sql).bind(...params).all();
    return new Response(JSON.stringify(results), { headers: { 'Content-Type': 'application/json' }});
  } catch (e) {
    return new Response(JSON.stringify([]), {status: 500});
  }
}

// === 2. 图片代理函数 ===
export async function proxyTelegramImage(fileId, botToken, dlExt = null) {
  try {
    const r1 = await fetch(`https://api.telegram.org/bot${botToken}/getFile?file_id=${fileId}`);
    const j1 = await r1.json();
    if (!j1.ok) return new Response("404", { status: 404 });

    const r2 = await fetch(`https://api.telegram.org/file/bot${botToken}/${j1.result.file_path}`);
    const h = new Headers(r2.headers);
    h.set("Cache-Control", "public, max-age=31536000, immutable");
    h.set("Access-Control-Allow-Origin", "*");

    if (dlExt) {
        const filename = `${fileId}.${dlExt}`;
        h.set("Content-Disposition", `attachment; filename="${filename}"`);
    }

    return new Response(r2.body, { headers: h });
  } catch (e) {
    return new Response("Error", { status: 500 });
  }
}

// === 3. 详情页处理函数 ===
export async function handleDetail(id, env) {
   const img = await env.DB.prepare("SELECT * FROM images WHERE id = ?").bind(id).first();
   if (!img) return new Response("404", { status: 404 });

   let parentId = img.id;
   const m = img.id.match(/^(.*)_p(\d+)$/);
   if (m) parentId = m[1];

   const { results: siblings } = await env.DB
     .prepare("SELECT * FROM images WHERE id = ? OR id LIKE ? ORDER BY id ASC")
     .bind(parentId, parentId + "_p%")
     .all();

   const { results: randomPosts } = await env.DB
     .prepare("SELECT * FROM images WHERE id != ? ORDER BY RANDOM() LIMIT 6")
     .bind(id)
     .all();

   const items = siblings.sort((a, b) => a.id.localeCompare(b.id));
   const currentIndex = Math.max(0, items.findIndex(x => x.id === img.id));
   const bgUrl = `/image/${img.file_name}`;
   const title = (img.caption || 'Untitled').split('\n')[0];
   const tags = (img.tags || '').trim().split(' ').filter(Boolean);

   const imagesJson = JSON.stringify(items.map(x => ({
     id: x.id,
     file: x.file_name,
     download: `/image/${x.origin_id || x.file_name}?dl=jpg`
   })));

   // 核心变化在这里：直接调用 templates.js 里的函数，而不是自己拼字符串
   const html = htmlDetail({
     title,
     bgUrl,
     imagesJson,
     currentIndex,
     tags,
     randomPosts
   });

   return new Response(html, {
     headers: { "Content-Type": "text/html;charset=UTF-8",'Cache-Control': 'public, max-age=60' }
   });
}
