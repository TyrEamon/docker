import { SIDEBAR_HTML, htmlDetail } from './templates.js';

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
   const img = await env.DB.prepare("SELECT id, file_name, origin_id, caption, artist, tags, created_at, width, height, artist FROM images WHERE id = ?").bind(id).first();
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
     artist: img.artist || '',
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

const BG_BLOCK_KEYWORDS = ['R-18','R18','NSFW','Hentai', 
  '性爱','性交','乱伦','裸胸','露点','调教',
  '触手','高潮','喷水','阿黑颜','颜射','后宫','痴汉',
  'NTR','3P','Creampie','Bukkake','Paizuri',
  '乳交', 'Cunnilingus','Fellatio','Masturbation',
  'Ahegao','X-ray','Mind Break','恶堕', 
  'Futa','Tentacle','BDSM','Bondage','Scat','Pregnant','naked','nipples','anus'];

// includeR18 = true  -> 不过滤（里世界）
// includeR18 = false -> 过滤 R18（安全）
export async function handleBgRandom(includeR18, url, env) {
  let sql = "SELECT * FROM images";
  let params = [];

  if (!includeR18) {
    const conditions = BG_BLOCK_KEYWORDS
      .map(() => "(tags NOT LIKE ? AND caption NOT LIKE ?)")
      .join(" AND ");
    sql += ` WHERE ${conditions}`;
    BG_BLOCK_KEYWORDS.forEach(k => {
      params.push(`%${k}%`);
      params.push(`%${k}%`);
    });
  }

  sql += " ORDER BY RANDOM() LIMIT 1";

  const { results } = await env.DB.prepare(sql).bind(...params).all();
  if (!results || results.length === 0) {
     return new Response("Not found", { status: 404 });
  }
 
  const fileId = results[0].file_name;

  // 导航站用：直接输出图片（Content-Type=image/*）
  if (url.searchParams.get('type') === 'image') {
    // 复用现有 Telegram 代理逻辑，强制 dlExt = 'jpg'
    return await proxyTelegramImage(fileId, env.BOT_TOKEN, 'jpg');
  }

  // 默认返回 JSON
  return new Response(JSON.stringify(results), {
    headers: { 'Content-Type': 'application/json' }
  });
}
