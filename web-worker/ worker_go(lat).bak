export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (url.hostname === 'www.mtcacg.top') {
      url.hostname = 'mtcacg.top';
      return Response.redirect(url.toString(), 301);
    }
    const path = url.pathname;

// 在处理 '/image/' 路由的地方
if (path.startsWith('/image/')) {
  // 获取 Cloudflare 默认缓存
  const cache = caches.default;
  
  // 1. 先查缓存
  let response = await cache.match(request);
  
  // 2. 如果没命中，才去请求 Telegram
  if (!response) {
    const fileId = path.replace('/image/', '');
    const dlExt = url.searchParams.get('dl'); 
    response = await proxyTelegramImage(fileId, env.BOT_TOKEN, dlExt);

    // 3. 只有成功请求(200 OK)才写入缓存
    if (response.status === 200) {
      // 这里的 clone() 很重要，因为 body 只能读一次
      ctx.waitUntil(cache.put(request, response.clone()));
    }
  }
  return response;
}

    if (path === '/api/posts') {
      const q = url.searchParams.get('q');
      const offset = url.searchParams.get('offset') || 0;
      
      if (q === 'random') {
        const { results } = await env.DB.prepare("SELECT * FROM images ORDER BY RANDOM() LIMIT 1").all();
        return new Response(JSON.stringify(results), { headers: { 'Content-Type': 'application/json' }});
      }

      // === 进阶搜索优化 ===
      let sql;
      let params = [];

      if (q) {
        // 去掉 # 号，按空格拆分，过滤掉空字符串
        const keywords = q.replace(/#/g, '').trim().split(/\s+/).filter(k => k.length > 0);

        if (keywords.length > 0) {
            // 2. 动态构建 SQL 条件
            // 对每个关键词生成一个 (tags LIKE ? OR caption LIKE ?) 条件
            // 并用 AND 连接，表示所有关键词都必须匹配（交集搜索）
            const conditions = keywords.map(() => `(tags LIKE ? OR caption LIKE ?)`).join(' AND ');
            
            sql = `
                SELECT * FROM images 
                WHERE ${conditions}
                ORDER BY created_at DESC 
                LIMIT 20 OFFSET ?
            `;

            // 3. 准备参数数组
            // 每个关键词需要绑定两次（一次给 tags，一次给 caption）
            keywords.forEach(k => {
                params.push(`%${k}%`); // 给 tags
                params.push(`%${k}%`); // 给 caption
            });
            params.push(offset); // 最后加上 offset
        } else {
            // 如果拆分后没有有效关键词（例如只输了空格），回退到默认
            sql = `SELECT * FROM images ORDER BY created_at DESC LIMIT 20 OFFSET ?`;
            params = [offset];
        }
      } else {
        // 无搜索词情况
        sql = `SELECT * FROM images ORDER BY created_at DESC LIMIT 20 OFFSET ?`;
        params = [offset];
      }
      // === 优化结束 ===

      try {
        const { results } = await env.DB.prepare(sql).bind(...params).all();
        return new Response(JSON.stringify(results), { headers: { 'Content-Type': 'application/json' }});
      } catch (e) {
        return new Response(JSON.stringify([]), {status: 500});
      }
    }

    if (path.match(/^\/detail\/(.+)$/)) return await handleDetail(path.match(/^\/detail\/(.+)$/)[1], env);

    if (path === '/about') return new Response(htmlAbout(), {headers: {'Content-Type': 'text/html;charset=UTF-8','Cache-Control': 'public, max-age=60'}});

    // 显式处理 /r18，复用主页模板
    if (path === '/r18') return new Response(htmlHome(), { headers: { 'Content-Type': 'text/html;charset=UTF-8','Cache-Control': 'public, max-age=60'}});

    return new Response(htmlHome(), { headers: { 'Content-Type': 'text/html;charset=UTF-8','Cache-Control': 'public, max-age=60'}});
  }
};

// 默认参数 dlExt = null，保证了不传参数时行为和原来一致
// 替换 proxyTelegramImage 函数

async function proxyTelegramImage(fileId, botToken, dlExt = null) {
  try {
    const r1 = await fetch(`https://api.telegram.org/bot${botToken}/getFile?file_id=${fileId}`);
    const j1 = await r1.json();
    if (!j1.ok) return new Response("404", { status: 404 });

    const r2 = await fetch(`https://api.telegram.org/file/bot${botToken}/${j1.result.file_path}`);
    const h = new Headers(r2.headers);
    h.set("Cache-Control", "public, max-age=31536000, immutable");
    h.set("Access-Control-Allow-Origin", "*");

    // ✅ 只要有 dl 参数，就强制赋予后缀，文件名就是 FileID.jpg
    if (dlExt) {
        const filename = `${fileId}.${dlExt}`;
        h.set("Content-Disposition", `attachment; filename="${filename}"`);
    }

    return new Response(r2.body, { headers: h });
  } catch (e) {
    return new Response("Error", { status: 500 });
  }
}

// 替换原有的 ...
// 【请完全替换代码最上方的 const SIDEBAR_HTML = ... 】
const SIDEBAR_HTML = `
<!-- 遮罩层 (修正层级 z-[200]) -->
<div id="sidebar-overlay" onclick="toggleSidebar()" class="fixed inset-0 bg-black/60 z-[200] hidden transition-opacity opacity-0" style="will-change: opacity"></div>

<!-- 侧边栏 (修正层级 z-[201]) -->
<aside id="sidebar" class="fixed top-0 left-0 w-72 h-full bg-[#1a1a1a] border-r border-white/10 z-[201] transform -translate-x-full transition-transform duration-300 ease-out shadow-2xl flex flex-col" style="will-change: transform">
  
  <div class="p-6 border-b border-white/10 flex items-center justify-between">
    <h2 class="text-2xl font-bold bg-gradient-to-r from-pink-500 to-purple-500 bg-clip-text text-transparent">MtcACG</h2>
    <button onclick="toggleSidebar()" class="text-gray-400 hover:text-white">&times;</button>
  </div>
  
  <nav class="flex-1 p-4 space-y-2">
    <a href="/" class="flex items-center p-3 text-gray-300 hover:bg-white/10 rounded-lg transition">
      <svg class="w-5 h-5 mr-3 text-gray-300" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M3 11.5L12 4l9 7.5M5 10.5V20h5v-5h4v5h5v-9.5"/></svg>
      <span>首页</span>
    </a>
    
    <!-- 随机抽图按钮 -->
    <a href="javascript:void(0)" onclick="randomPost(); toggleSidebar();" class="flex items-center p-3 text-gray-300 hover:bg-white/10 rounded-lg transition">
      <svg class="w-5 h-5 mr-3 text-gray-300" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M4 6h4l3 6 3-6h4M4 18h4l3-6 3 6h4"/></svg>
      <span>随机抽图看看0w0</span>
    </a>

    <a href="/r18" class="flex items-center p-3 text-red-300 hover:bg-red-500/10 rounded-lg transition group">
      <svg class="w-5 h-5 mr-3 text-red-400 group-hover:text-red-200" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>
      <span class="font-bold">里世界 · 纯R18|慎</span>
    </a>
    
    <a href="/about" class="flex items-center p-3 text-gray-300 hover:bg-white/10 rounded-lg transition">
       <svg class="w-5 h-5 mr-3 text-gray-300" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" stroke-linecap="round" stroke-linejoin="round"/><path stroke-linecap="round" stroke-linejoin="round" d="M12 8.5v.01M11 11h1v5h1"/></svg>
       <span>关于</span>
    </a>
    
    <div class="pt-4 mt-4 border-t border-white/10">
      <div class="flex items-center justify-between p-3">
         <span class="text-gray-300 flex items-center"><span class="mr-3">🔞</span> R18 哒咩~</span>
         <label class="relative inline-flex items-center cursor-pointer">
           <!-- 注意：这里改了 ID 防止冲突，并绑定全局开关函数 -->
           <input type="checkbox" id="r18-toggle-sidebar" class="sr-only peer" onchange="toggleR18Global(this)">
           <div class="w-11 h-6 bg-gray-600 peer-focus:outline-none rounded-full peer 
           after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all 
           after:translate-x-full peer-checked:after:translate-x-0 
           bg-pink-600 peer-checked:bg-gray-600"></div>
      
         </label>
      </div>
    </div>
  </nav>

  <!-- Friends/GitHub 链接 -->
  <div class="pt-4 mt-4 border-t border-white/10">
      <p class="px-3 text-xs font-bold text-gray-500 uppercase mb-2">Friends</p>
      <a href="https://github.com/TyrEamon/MtcACG-GO" target="_blank" class="flex items-center p-3 text-gray-400 hover:text-white hover:bg-white/5 rounded-lg text-sm">
         <svg class="w-4 h-4 mr-2" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clip-rule="evenodd"/></svg>
         GitHub
      </a>
  </div>
  <div class="p-4 text-xs text-center text-gray-600 border-t border-white/5">© 2025 MtcACG Gallery</div>
</aside>

<script>
// --- 开始 ---
async function randomImage() {
   const res = await fetch('/api/posts?q=random');
   const data = await res.json();
   if(data.length) window.location.href = '/detail/' + data[0].id;
}
// --- 结束 ---
  function toggleSidebar() {
    const sb = document.getElementById('sidebar');
    const ov = document.getElementById('sidebar-overlay');
    if(!sb || !ov) return;
    
    // 逻辑修正：检查 class 是否存在
    const isOpen = !sb.classList.contains('-translate-x-full');
    
    if (isOpen) {
      sb.classList.add('-translate-x-full');
      ov.classList.remove('opacity-100');
      setTimeout(() => ov.classList.add('hidden'), 300);
    } else {
      ov.classList.remove('hidden');
      void ov.offsetWidth; // 触发 transition
      ov.classList.add('opacity-100');
      sb.classList.remove('-translate-x-full');
    }
  }

  function toggleR18Global(el) {
    localStorage.setItem('hide_r18', !el.checked);
    location.reload();
  }
  
  // 初始化开关状态
  setTimeout(() => {
    const toggle = document.getElementById('r18-toggle-sidebar');
    if(toggle) {
        toggle.checked = (localStorage.getItem('hide_r18') !== 'true');
    }
}, 100);
  

  async function randomPost() {
    try { 
      const res = await fetch('/api/posts?q=random'); 
      const data = await res.json(); 
      if(data.length) location.href='/detail/'+data[0].id; 
    } catch(e){}
  }
</script>
`;


// 详情页：精简头部（仅保留菜单按钮）+ 侧边栏防透视修复
async function handleDetail(id, env) {
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

// 在 handleDetail 函数中
   const imagesJson = JSON.stringify(items.map(x => ({
     id: x.id,
     file: x.file_name,
  // ✅ 暴力写法：不管你是预览图还是原图，统统加上 ?dl=jpg
  // 这样下载下来的文件名就是：文件ID.jpg
     download: `/image/${x.origin_id || x.file_name}?dl=jpg`
   })));


  // 侧边栏 HTML (背景 bg-[#1a1a1a] 不透明，防止花屏
  const SIDEBAR_CONTENT = `
    <div id="overlay" onclick="toggleSidebar()" class="fixed inset-0 bg-black/60 z-[99] hidden transition-opacity opacity-0" style="will-change: opacity"></div>
    <aside id="sidebar" class="fixed top-0 left-0 w-72 h-full bg-[#1a1a1a] border-r border-white/10 z-[100] transform -translate-x-full transition-transform duration-300 ease-out shadow-2xl flex flex-col" style="will-change: transform">
      <div class="p-6 border-b border-white/10 flex items-center justify-between">
        <h2 class="text-2xl font-bold bg-gradient-to-r from-pink-500 to-purple-500 bg-clip-text text-transparent">MtcACG</h2>
        <button onclick="toggleSidebar()" class="text-gray-400 hover:text-white">&times;</button>
      </div>
      <nav class="flex-1 p-4 space-y-2">
        <a href="/" class="flex items-center p-3 text-gray-300 hover:bg-white/10 rounded-lg transition">
          <svg class="w-5 h-5 mr-3 text-gray-300" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M3 11.5L12 4l9 7.5M5 10.5V20h5v-5h4v5h5v-9.5"/></svg>
          <span>首页</span>
        </a>
        <!-- 新增：随机抽图按钮 -->
        <a href="javascript:void(0)" onclick="randomImage(); toggleSidebar();" class="flex items-center p-3 text-gray-300 hover:bg-white/10 rounded-lg transition">
          <svg class="w-5 h-5 mr-3 text-gray-300" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M4 6h4l3 6 3-6h4M4 18h4l3-6 3 6h4"/></svg>
          <span>随机抽图看看0w0</span>
        </a>
        <!-- 新增：里世界入口 -->
        <a href="/r18" class="flex items-center p-3 text-red-300 hover:bg-red-500/10 rounded-lg transition group">
          <svg class="w-5 h-5 mr-3 text-red-400 group-hover:text-red-200" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>
          <span class="font-bold">里世界 · 纯R18|慎</span>
        </a>    
        <a href="/about" class="flex items-center p-3 text-gray-300 hover:bg-white/10 rounded-lg transition">
           <svg class="w-5 h-5 mr-3 text-gray-300" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" stroke-linecap="round" stroke-linejoin="round"/><path stroke-linecap="round" stroke-linejoin="round" d="M12 8.5v.01M11 11h1v5h1"/></svg>
           <span>关于</span>
        </a>
        <div class="pt-4 mt-4 border-t border-white/10">
          <div class="flex items-center justify-between p-3">
             <span class="text-gray-300 flex items-center"><span class="mr-3">🔞</span> R18 哒咩~</span>
             <label class="relative inline-flex items-center cursor-pointer">
               <input type="checkbox" id="r18-toggle-sidebar" class="sr-only peer" onchange="toggleR18(this)">
               <div class="w-11 h-6 bg-gray-600 peer-focus:outline-none rounded-full peer 
               after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all 
               after:translate-x-full peer-checked:after:translate-x-0 
               bg-pink-600 peer-checked:bg-gray-600"></div>          
             </label>
          </div>
        </div>
      </nav>
      <!-- 这里加上了 Friends/GitHub 链接，保持一致 -->
      <div class="pt-4 mt-4 border-t border-white/10">
          <p class="px-3 text-xs font-bold text-gray-500 uppercase mb-2">Friends</p>
          <a href="https://github.com/TyrEamon/MTCacg" target="_blank" class="flex items-center p-3 text-gray-400 hover:text-white hover:bg-white/5 rounded-lg text-sm">
             <svg class="w-4 h-4 mr-2" fill="currentColor" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" clip-rule="evenodd"/></svg>
             GitHub
          </a>
      </div>
      <div class="p-4 text-xs text-center text-gray-600 border-t border-white/5">© 2025 MtcACG Gallery</div>
    </aside>
  `;

  return new Response(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>${title}</title>
  <link rel="icon" type="image/png" href="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/ACGg.png">
  <script src="https://cdn.tailwindcss.com"></script>
  <style>
    html, body { 
      margin: 0; padding: 0; width: 100%; height: 100%; 
      background: #050509; color: #fff; font-family: system-ui, sans-serif;
      overflow: hidden; 
    }
    
    #fixed-bg {
      position: fixed; inset: 0; z-index: 1;
      background-image: url('${bgUrl}');
      background-size: cover; background-position: center;
      filter: blur(7px) brightness(0.85);
      transform: scale(1.1);
      opacity: 0.7; 
      pointer-events: none;
    }

    .header { position: fixed; top: 0; left: 0; right: 0; z-index: 50; padding: 12px 20px; display: flex; justify-content: space-between; align-items: center; }
    
    /* 玻璃按钮样式 */
    .glass-btn { 
      display: flex; align-items: center; justify-content: center;
      color: #ccc; text-decoration: none; 
      background: rgba(0,0,0,0.4); padding: 8px 12px; 
      border-radius: 99px; backdrop-filter: blur(4px); transition: .2s; 
      cursor: pointer; border: none;
    }
    .glass-btn:hover { color: #fff; background: rgba(255,255,255,0.1); }
    
    .brand { font-weight: 800; opacity: 0.8; }

    .page-container { position: relative; z-index: 2; display: flex; flex-direction: column; height: 100vh; padding-top: 60px; box-sizing: border-box; }
    .main-content {
      display: flex; flex: 1; padding: 20px; gap: 40px;
      overflow: hidden; width: 100%; max-width: 1400px;
      margin: 0 auto; box-sizing: border-box;
    }
    
    .left-col { flex: 1.8; display: flex; align-items: center; justify-content: center; position: relative; min-width: 0; height: 100%; }
    .right-col {
      flex: 1; display: flex; flex-direction: column; gap: 20px;
      overflow-y: auto; min-height: 0; padding-right: 10px;
      max-width: 450px;
    }
    .right-col::-webkit-scrollbar { width: 6px; }
    .right-col::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.2); border-radius: 3px; }

    @media(max-width: 1023px) {
      body { overflow-y: auto; }
      .page-container { height: auto; padding-top: 50px; display: block; }
      .main-content { display: block; padding: 16px; overflow: visible; height: auto; gap: 20px; }
      .left-col { height: auto; min-height: 300px; margin-bottom: 24px; }
      .right-col { overflow: visible; height: auto; padding-right: 0; max-width: 100%; }
    }

    .viewer { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; position: relative; }
    .viewer img { 
      max-width: 100%; max-height: 100%; 
      object-fit: contain; border-radius: 12px; 
      box-shadow: 0 10px 40px rgba(0,0,0,0.5); 
    }
    @media(max-width: 1023px) { .viewer img { max-height: 60vh; } }

    .nav-btn { position: absolute; top: 50%; transform: translateY(-50%); width: 44px; height: 44px; border-radius: 50%; background: rgba(0,0,0,0.5); color: #fff; display: flex; align-items: center; justify-content: center; cursor: pointer; font-size: 20px; border: 1px solid rgba(255,255,255,0.1); z-index: 10; }
    .nav-btn.prev { left: 16px; } .nav-btn.next { right: 16px; }

    .info-box { background: rgba(30,30,35,0.5); backdrop-filter: blur(20px); border-radius: 20px; padding: 24px; border: 1px solid rgba(255,255,255,0.08); flex-shrink: 0; }
    h1 { font-size: 20px; margin: 0 0 8px; line-height: 1.4; word-break: break-word; }
    .id-row { font-family: monospace; color: #888; font-size: 13px; margin-bottom: 20px; }
    .tags { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 24px; }
    .tag { background: rgba(255,255,255,0.08); color: #ccc; padding: 6px 12px; border-radius: 8px; font-size: 13px; text-decoration: none; transition: .2s; }
    .tag:hover { background: rgba(255,255,255,0.2); color: #fff; }
    
    .dl-btn { display: block; width: 100%; box-sizing: border-box; text-align: center; background: linear-gradient(90deg, #ec4899, #8b5cf6); color: #fff; font-weight: 700; text-decoration: none; padding: 14px; border-radius: 12px; transition: .2s; }
    .dl-btn:hover { transform: translateY(-2px); filter: brightness(1.1); }
    
    .rec-grid { display: grid; gap: 12px; grid-template-columns: repeat(2, 1fr); }
    @media(min-width: 1024px) { .rec-grid { grid-template-columns: repeat(3, 1fr); } }
    .rec-item { aspect-ratio: 1; border-radius: 12px; overflow: hidden; background: #000; }
    .rec-item img { width: 100%; height: 100%; object-fit: cover; transition: .3s; }
    .rec-item:hover img { opacity: 0.8; transform: scale(1.05); }

    #lightbox { position: fixed; inset: 0; background: rgba(0,0,0,0.9); display: none; align-items: center; justify-content: center; z-index: 100; }
  </style>
</head>
<body>
  <div id="fixed-bg"></div>
  ${SIDEBAR_CONTENT}

  <div class="page-container">
    <div class="header">
      <!-- 仅保留侧边栏按钮 -->
      <button onclick="toggleSidebar()" class="glass-btn">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 12h18M3 6h18M3 18h18"/></svg>
      </button>
      <div class="brand">MtcACG</div>
    </div>

    <!-- 全屏大图查看层 -->
    <div id="lightbox">
      <img id="lightbox-img" src="" style="max-width: 95vw; max-height: 95vh; object-fit: contain; box-shadow: 0 20px 60px rgba(0,0,0,0.8); border-radius: 12px;">
    </div>
  
    <div class="main-content" id="app"
        data-images='${imagesJson.replace(/'/g, "\\'")}'
        data-index='${currentIndex}'
        data-title='${title.replace(/'/g, "\\'")}'>
      
      <div class="left-col">
        <div class="viewer">
          <img id="img" src="" alt="">
          <div class="nav-btn prev" style="display:none" onclick="go(-1)">❮</div>
          <div class="nav-btn next" style="display:none" onclick="go(1)">❯</div>
        </div>
      </div>

      <div class="right-col">
        <div class="info-box">
          <h1 id="title-text"></h1>
          <div id="id-text" class="id-row"></div>
          <div id="tags-box" class="tags">${tags.map(t => `<a href="/?q=${encodeURIComponent(t)}" class="tag">#${t}</a>`).join('')}</div>
          <a id="dl-link" href="#" target="_blank" class="dl-btn">Download Original</a>
        </div>
        
        <div class="rec-grid">
          ${randomPosts.map(p => `<a href="/detail/${p.id}" class="rec-item"><img src="/image/${p.file_name}" loading="lazy"></a>`).join('')}
        </div>
      </div>
    </div>
  </div>

  <script>
  // --- ✅ 从这里开始插入 ---
    async function randomImage() {
      try {
        const res = await fetch('/api/posts?q=random');
        const data = await res.json();
        if(data.length) window.location.href = '/detail/' + data[0].id;
      } catch(e) {}
    }
    // --- 🏁 插入结束 ---
    function toggleSidebar() {
      const sb = document.getElementById('sidebar');
      const ov = document.getElementById('overlay');
      if(!sb || !ov) return;
      const isOpen = !sb.classList.contains('-translate-x-full');
      if (isOpen) {
        sb.classList.add('-translate-x-full');
        ov.classList.remove('opacity-100');
        setTimeout(() => ov.classList.add('hidden'), 300);
      } else {
        ov.classList.remove('hidden');
        void ov.offsetWidth;
        ov.classList.add('opacity-100');
        sb.classList.remove('-translate-x-full');
      }
    }
    function toggleR18(el) {
      localStorage.setItem('hide_r18', !el.checked);
      location.reload();
    }
    setTimeout(() => {
      const toggle = document.getElementById('r18-toggle-sidebar');
      if(toggle) {
          toggle.checked = (localStorage.getItem('hide_r18') !== 'true');
      }
  }, 100);  
  

    const root = document.getElementById('app');
    const images = JSON.parse(root.dataset.images);
    let idx = parseInt(root.dataset.index);
    const imgEl = document.getElementById('img');
    const titleEl = document.getElementById('title-text');
    const idEl = document.getElementById('id-text');
    const dlLink = document.getElementById('dl-link');
    const btnPrev = document.querySelector('.nav-btn.prev');
    const btnNext = document.querySelector('.nav-btn.next');
    const lightbox = document.getElementById('lightbox');
    const lightboxImg = document.getElementById('lightbox-img');

    imgEl.addEventListener('click', () => {
      if(!images.length) return;
      lightboxImg.src = '/image/' + images[idx].file;
      lightbox.style.display = 'flex';
    });
    lightbox.addEventListener('click', () => {
      lightbox.style.display = 'none';
      lightboxImg.src = '';
    });
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape' && lightbox.style.display === 'flex') {
        lightbox.style.display = 'none';
        lightboxImg.src = '';
      }
    });
   
    if (images.length > 1) { btnPrev.style.display = 'flex'; btnNext.style.display = 'flex'; }

    function render(dir) {
      const item = images[idx];
      if(dir) imgEl.style.opacity = '0.5';
      setTimeout(() => {
        imgEl.src = '/image/' + item.file;
        titleEl.textContent = root.dataset.title + (images.length > 1 ? \` [P\${idx+1}/\${images.length}]\` : '');
        idEl.textContent = 'ID: ' + item.id;
        dlLink.href = item.download;
        imgEl.onload = () => imgEl.style.opacity = '1';
      }, dir ? 50 : 0);
    }
    render(0);

    window.go = (dir) => { idx = (idx + dir + images.length) % images.length; render(dir); };
    document.addEventListener('keydown', e => { if(e.key === 'ArrowLeft') go(-1); if(e.key === 'ArrowRight') go(1); });
  </script>
</body>
</html>`, { headers: { "Content-Type": "text/html;charset=UTF-8",'Cache-Control': 'public, max-age=60' } });
}

function htmlAbout() {
  return `
  <!DOCTYPE html>
  <html class="dark">
  <head>
    <meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>About - MtcACG</title>
    <link rel="icon" type="image/png" href="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/ACGg.png">
    <script src="https://cdn.tailwindcss.com"></script>
    <link rel="icon" href="data:image/svg+xml,<svg xmlns=%22http://www.w3.org/2000/svg%22 viewBox=%220 0 100 100%22><text y=%22.9em%22 font-size=%2290%22>🌸</text></svg>">
    <style>
      /* 动态背景 */
      #bg-layer { 
        position: fixed; top: 0; left: 0; width: 100vw; height: 100vh; z-index: -1; 
        background-size: cover; background-position: center; 
        filter: blur(8px) brightness(0.6); 
        transition: opacity 1s; opacity: 0; 
        transform: translate3d(0,0,0); will-change: opacity; pointer-events: none;
      }
      
      /* 隐藏滚动条但允许滚动 */
      .no-scrollbar::-webkit-scrollbar { display: none; }
      .no-scrollbar { -ms-overflow-style: none; scrollbar-width: none; }
      
      /* 玻璃板内的文字排版 */
      .content-box h2 { font-size: 1.25rem; font-weight: 700; margin-bottom: 1rem; color: #fff; border-left: 4px solid #ec4899; padding-left: 12px; margin-top: 2rem; }
      .content-box p { margin-bottom: 1rem; line-height: 1.7; color: #e5e7eb; }
      .content-box code { background: rgba(255,255,255,0.15); padding: 2px 6px; border-radius: 4px; font-family: monospace; color: #f9a8d4; }
      .content-box a { color: #f472b6; text-decoration: none; border-bottom: 1px dashed #f472b6; transition: color 0.2s; }
      .content-box a:hover { color: #fff; border-bottom-style: solid; }
    </style>
  </head>
  <body class="bg-gray-900 text-white min-h-screen flex items-center justify-center p-4 overflow-hidden">
    <div id="bg-layer"></div>
    ${SIDEBAR_HTML}
    
    <!-- 🟢 玻璃板容器 -->
    <div class="max-w-2xl w-full bg-black/40 backdrop-blur-xl p-6 md:p-10 rounded-3xl shadow-2xl relative border border-white/10 content-box h-[85vh] overflow-y-auto no-scrollbar">
       
       <!-- 顶部栏 -->
       <div class="flex items-center justify-between mb-8 sticky top-0 z-10 py-4 -mx-6 px-6 -mt-6 border-b border-white/5 bg-black/20 backdrop-blur-md">
         <div class="flex items-center gap-4">
           <button onclick="toggleSidebar()" class="text-gray-300 hover:text-white transition p-1">
             <svg width="24" height="24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12h18M3 6h18M3 18h18"/></svg>
           </button>
           <h1 class="text-2xl font-bold bg-gradient-to-r from-pink-500 to-purple-500 bg-clip-text text-transparent">关于 MtcACG</h1>
         </div>
         <a href="/" class="text-xs bg-white/10 hover:bg-white/20 px-3 py-1.5 rounded-full transition border border-white/5">回到首页</a>
       </div>

       <!-- 序言 -->
       <section class="animate-fade-in">
         <h2 class="text-xl font-medium text-white mb-3 flex items-center !mt-0">
           <span class="w-1 h-6 bg-pink-500 rounded-full mr-3 opacity-80"></span>
           序 · Start
         </h2>
         <p>
         欢迎来到 MtcACG！(≧∇≦)ﾉ
         在乱糟糟的互联网异世界里，这里是本站长偷偷搭建的“秘密基地”。
         </p>
         <p class="mt-2">
         这里没有算法裹挟，只有我的私人凝视。每一张图，都是我从时间里切下的碎片，安放于此，建成一座只属于我的数字花园。
         </p>
       </section>
     
       <!-- 功能 -->
       <section>
         <h2 class="text-xl font-medium text-white mb-3 flex items-center">
           <span class="w-1 h-6 bg-purple-500 rounded-full mr-3 opacity-80"></span>
           逛 · Explore
         </h2>
         <p>  
         想怎么玩都行！跟着 <code>#标签</code> 寻找同好，或在 <code>瀑布流</code> 里无限滑行。
         还有哦，左上角的菜单里藏着通往“里世界”的钥匙——那是 <strong>R-18 </strong> 的封印。但还是要保持绅士风度哦 (/ω＼)。
         </p>
       </section>
     
       <!-- 接口 -->
       <section>
         <h2 class="text-xl font-medium text-white mb-3 flex items-center">
           <span class="w-1 h-6 bg-blue-500 rounded-full mr-3 opacity-80"></span>
           连 · Link
         </h2>
         <p>
         想把这里的风景带回你的世界？和你定下一个数据契约吧：
           <br>
           <code class="text-sm bg-black/30 px-3 py-2 rounded-lg mt-3 block w-full md:w-auto font-mono text-pink-300 border border-white/5 select-all">/api/posts?q=random</code>
           <br>
           这是一个随机召唤阵，每次点击，都会召唤出一张此时此刻的惊喜。
         </p>
       </section>
     
       <!-- 尾声 -->
       <section>
         <h2 class="text-xl font-medium text-white mb-3 flex items-center">
           <span class="w-1 h-6 bg-gray-500 rounded-full mr-3 opacity-80"></span>
           寄语 · Epilogue
         </h2>
         <p>
         现在的 MtcACG 还在长身体的阶段呢，很多想收录的美图还在排队等着“入驻”。欢迎通过<a href="https://t.me/trytwosBot" target="_blank">Telegram</a> 投递信件。
         “请多给这个小小的图库一点耐心和爱吧~”
         </p>
         <p class="mt-6 text-sm opacity-60 italic text-center">
           "愿你在这里，捕获到最让你心动的那一抹色彩."
         </p>
       </section>

       <div class="mt-12 pt-8 border-t border-white/10 text-center text-xs text-gray-500 font-mono">
         © 2025 MtcACG Gallery <span class="mx-2">|</span> Powered by Cloudflare Workers
       </div>

    </div>

    <script>
      // 自动拉取一张随机图作为背景
      async function setRandomBg() {
        try {
          const res = await fetch('/api/posts?q=random');
          const data = await res.json();
          if(data.length) {
            const bg = document.getElementById('bg-layer');
            bg.style.backgroundImage = 'url(/image/' + data[0].file_name + ')';
            bg.style.opacity = '1';
          }
        } catch(e){}
      }
      setRandomBg();
    </script>
  </body>
  </html>`;
}

// 首页：JS 动态 Masonry 版（绝对多列，不再依赖 CSS Columns）
// 首页：JS 动态 Masonry 版
function htmlHome() {
  return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
  <title>MtcACG</title>
  <link rel="icon" type="image/png" href="https://pub-d07d03b8c35d40309ce9c6d8216e885b.r2.dev/ACGg.png">
  <script src="https://cdn.tailwindcss.com"></script>
  <style>
    /* 隐藏滚动条但保留功能 */
    ::-webkit-scrollbar { width: 0px; background: transparent; }
    html { -ms-overflow-style: none; scrollbar-width: none; }
    body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #121212; color: #fff; overflow-x: hidden; }
    #bg-layer { position: fixed; inset: 0; z-index: -1; background-size: cover; background-position: center; filter: blur(6px) brightness(0.6); opacity: 0; transition: opacity 1s; pointer-events: none; }
    .header { position: fixed; top: 0; left: 0; right: 0; z-index: 28; background: rgba(18, 18, 18, 0.90); backdrop-filter: none; -webkit-backdrop-filter: none; border-bottom: 1px solid rgba(255,255,255,0.1); padding: 12px 16px; display: flex; align-items: center; justify-content: space-between; }
    .logo { font-weight: 800; font-size: 18px; letter-spacing: 1px; color: #fff; text-decoration: none; }
    .search-bar { flex: 1; max-width: 400px; margin: 0 16px; position: relative; }
    input { background: rgba(255,255,255,0.05); border: 1px solid rgba(255,255,255,0.15); color: white; padding: 8px 16px; border-radius: 99px; width: 100%; outline: none; transition: 0.3s; font-size: 14px; }
    input:focus { background: rgba(0,0,0,0.6); border-color: #ec4899; }
    .masonry-wrap { display: flex; gap: 12px; padding: 12px; align-items: flex-start; }
    @media(min-width: 768px) { .masonry-wrap { padding: 20px; gap: 20px; max-width: 1800px; margin: 0 auto; } }
    .masonry-col { flex: 1; display: flex; flex-direction: column; gap: 12px; min-width: 0; }
    @media(min-width: 768px) { .masonry-col { gap: 20px; } }
    .card { border-radius: 12px; overflow: hidden; background: #2a2a2a; position: relative; transition: transform 0.2s ease-out; box-shadow: 0 4px 6px rgba(0,0,0,0.3); width: 100%; }
    .card:active { transform: scale(0.98); }
    .card:hover { transform: scale(1.02) translateY(-4px); z-index: 20; box-shadow: 0 16px 24px -6px rgba(0,0,0,0.6); }
    .card-inner { position: relative; width: 100%; }
    .placeholder { display: block; width: 100%; padding-bottom: calc(var(--h) / var(--w) * 100%); background: #2a2a2a; }
    .card-img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; opacity: 0; transition: opacity .3s; }
    .card-img.loaded { opacity: 1; }
    .meta { position: absolute; bottom: 0; left: 0; right: 0; padding: 50px 10px 10px; background: linear-gradient(to top, rgba(0,0,0,0.8), transparent); opacity: 0; transition: opacity 0.2s; }
    .card:hover .meta { opacity: 1; }
    @media(max-width: 768px) { .meta { padding: 30px 8px 8px; opacity: 1; } .title { font-size: 11px; } }
    .title { font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-shadow: 0 1px 3px rgba(0,0,0,0.9); color: #fff; }
    .loading-tip { position: fixed; bottom: 20px; left: 50%; transform: translateX(-50%); background: rgba(0,0,0,0.7); color: #fff; padding: 6px 16px; border-radius: 99px; font-size: 12px; backdrop-filter: blur(5px); opacity: 0; transition: opacity .2s; pointer-events: none; z-index: 100; }
  </style>
</head>
<body>
  <div id="bg-layer"></div>
  ${SIDEBAR_HTML}
  
  <div class="header">
    <div class="p-2 cursor-pointer" onclick="toggleSidebar()">
      <svg width="24" height="24" fill="none" stroke="white" stroke-width="2"><path d="M3 12h18M3 6h18M3 18h18"/></svg>
    </div>
    <div class="search-bar">
      <input type="text" id="search" placeholder="  要搜索什么吖...." onchange="doSearch(this.value)">
    </div>
    <a href="/" class="logo">MtcACG</a>
  </div>

  <div id="masonry" class="masonry-wrap"></div>
  <!-- ✅ 从这里开始插入 -->
  <button id="auto-scroll-btn" onclick="toggleAutoScroll()" 
      class="fixed bottom-8 right-8 z-50 w-12 h-12 rounded-full 
             bg-white/10 hover:bg-white/20 backdrop-blur-md 
             border border-white/10 shadow-xl 
             text-gray-300 hover:text-white 
             transition-all duration-300 transform hover:scale-105 active:scale-95 flex items-center justify-center group"
      title="自动滚动">
     <!-- 播放图标 -->
     <svg id="icon-play" class="w-5 h-5 ml-0.5 group-hover:text-pink-400 transition-colors" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
     <!-- 暂停图标 -->
     <svg id="icon-pause" class="w-5 h-5 hidden text-pink-500 animate-pulse" fill="currentColor" viewBox="0 0 24 24"><path d="M6 19h4V5H6v14zm8-14v14h4V5h-4z"/></svg>
  </button>
  <!-- 🏁 插入结束 -->
  <div id="tip" class="loading-tip">在加载啦…别、别急呀喵～</div>

  <script>
    const masonry = document.getElementById('masonry');
    const bgLayer = document.getElementById('bg-layer');
    const tip = document.getElementById('tip');
    const hideR18 = localStorage.getItem('hide_r18') === 'true';

    let offset = 0;
    const params = new URLSearchParams(window.location.search);
    let q = params.get('q') || ''; 
    if(q) {
        document.addEventListener('DOMContentLoaded', () => {
             const searchInput = document.getElementById('search');
             if(searchInput) searchInput.value = q;
        });
    }
    
    let isLoading = false;
    let done = false;
    let colCount = window.innerWidth < 768 ? 2 : (window.innerWidth < 1200 ? 3 : 4);
    let cols = [];
    let colHeights = []; // 用于记录每列的预估高度

    function initMasonry() {
      masonry.innerHTML = '';
      cols = [];
      colHeights = new Array(colCount).fill(0); // <--- 新增这行，重置高度
      for(let i=0; i<colCount; i++) {
        const div = document.createElement('div');
        div.className = 'masonry-col';
        masonry.appendChild(div);
        cols.push(div);
      }
    }
    
    window.addEventListener('resize', () => {
      const newCount = window.innerWidth < 768 ? 2 : (window.innerWidth < 1200 ? 3 : 4);
      if(newCount !== colCount) {
        colCount = newCount;
        offset = 0;
        load(true);
      }
    });

    // ===========================================
    // --- 2. 智能自动滚动逻辑 (独立于 load 函数外) ---
    // ===========================================
    let autoScrollActive = false;
    let scrollTimer = null;
    let resumeTimer = null;

    function getIcons() {
      return {
        btn: document.getElementById('auto-scroll-btn'),
        play: document.getElementById('icon-play'),
        pause: document.getElementById('icon-pause')
      };
    }

    window.toggleAutoScroll = function() {
      autoScrollActive = !autoScrollActive;
      if (autoScrollActive) {
        updateBtnState(true);
        startScrolling();
      } else {
        updateBtnState(false);
        stopScrolling();
        if (resumeTimer) { clearTimeout(resumeTimer); resumeTimer = null; }
      }
    }

    function updateBtnState(isActive) {
      const { btn, play, pause } = getIcons();
      if(!btn) return;
      if(isActive) {
        btn.classList.add('bg-white/20', 'border-pink-500/50', 'shadow-pink-500/20');
        btn.classList.remove('border-white/10');
        play.classList.add('hidden'); pause.classList.remove('hidden');
      } else {
        btn.classList.remove('bg-white/20', 'border-pink-500/50', 'shadow-pink-500/20');
        btn.classList.add('border-white/10');
        play.classList.remove('hidden'); pause.classList.add('hidden');
      }
    }

    function startScrolling() {
      if (!autoScrollActive) return;
      if (scrollTimer) clearInterval(scrollTimer);
      scrollTimer = setInterval(() => {
        window.scrollBy(0, 0.8);
        if(done && (window.innerHeight + window.scrollY) >= document.body.offsetHeight) {
             window.toggleAutoScroll(); 
        }
      }, 16);
    }

    function stopScrolling() {
      if (scrollTimer) { clearInterval(scrollTimer); scrollTimer = null; }
    }

    ['mousedown', 'wheel', 'touchstart', 'keydown'].forEach(event => {
      window.addEventListener(event, () => {
        if (autoScrollActive) {
          stopScrolling();
          if (resumeTimer) clearTimeout(resumeTimer);
          resumeTimer = setTimeout(() => {
            if (autoScrollActive) startScrolling();
          }, 1500);
        }
      }, { passive: true });
    });
    

    async function load(reset = false) {
      if (isLoading || (done && !reset)) return;
      isLoading = true;
      tip.style.opacity = '1';

      if (reset) {
        initMasonry();
        offset = 0;
        done = false;
      } else if (cols.length === 0) {
        initMasonry();
      }

      try {
        // 注意：反引号被转义
        const res = await fetch(\`/api/posts?offset=\${offset}&q=\${encodeURIComponent(q)}\`);
        const data = await res.json();

        if (data.length === 0) {
          done = true;
          if(offset > 0) tip.textContent = '已经到底啦 (｡•ˇ‸ˇ•｡)';
          setTimeout(() => tip.style.opacity = '0', 2000);
          isLoading = false;
          return;
        }
  
        let colHeights = new Array(colCount).fill(0);
        
    const blockKeywords =[
        'R-18','NSFW','Hentai','血腥','R18','性爱','性交','淫','乱伦','裸胸',
        '露点','调教','捆绑','触手','高潮','喷水','阿黑颜','颜射','后宫','痴汉','NTR','3P','Boobs','Tits',
        'Nipples','Breast','强暴','做爱','自慰','援交','喷水','Creampie','Cum','Bukkake','Sex','Fuck',
        'Blowjob','口交','Handjob','Paizuri','乳交','Cunnilingus','Fellatio','Masturbation','Pussy',
        'Vagina','Penis','Dick','Cock','Genitals','Pubic','阴部','阴茎','私处','白虎','爆乳','Breast',
        'Nude','Topless','Ahegao','高潮脸','X-ray','断面图','Mind Break','恶堕','坏掉','透视','Futa','扶她',
        '双性','Tentacle','BDSM','Bondage','束缚','Scat','Pregnant','妊娠','怀孕','异种','丸吞','破れタイツ',
        '敗北','快楽堕ち','寝取られ','乳出し','Garter','Lingerie','Panty','Stockings','ふたなり','輪姦','母子',
        '近親','異種姦','孕ませ','緊縛','奴隷','悪堕ち','精神崩壊','セックス','中出し','顔射','イラマチオ','フェラ',
        'パイズリ','手コキ','潮吹き','絶頂','アヘ顔','全裸','乳首','ペニス','ヴァギナ','クリトリス','近親','触手',
        'レイプ','調教','スカトロ','ふたなり','パンツ下ろし','naked','nipples','anus',];

    const r18Keywords = [
        'R-18','NSFW','Hentai','血腥','R18','性爱','性交','淫','乱伦','裸胸','露点','调教',
        '捆绑','触手','高潮','喷水','阿黑颜','颜射','后宫','痴汉','NTR','3P','Boobs','Tits','Nipples','Breast','强暴',
        '做爱','自慰','援交','喷水','Creampie','Cum','Bukkake','Sex','Fuck','Blowjob','口交','Handjob','Paizuri',
        '乳交','Cunnilingus','Fellatio','Masturbation','Pussy','Vagina','Penis','Dick','Cock','Genitals','Pubic',
        '阴部','阴茎','私处','白虎','爆乳','Breast','Nude','Topless','Ahegao','高潮脸','X-ray','断面图','Mind Break',
        '恶堕','坏掉','透视','Futa','扶她','双性','Tentacle','BDSM','Bondage','捆绑','束缚','Scat','Pregnant','妊娠',
        '怀孕','异种','绳艺','丸吞','破れタイツ','敗北','快楽堕ち','寝取られ','乳出し','パンツ下ろし','尻揉み','比基尼','裸足',
        'School Swimsuit','アナル尻尾','Maid','Swimsuit','Ass','成人','成人','Pantyhose','Garter','连裤袜','ロリ',
        'Lingerie','Panty','Stockings','ふたなり','輪姦','母子','近親','異種姦','孕ませ','緊縛','奴隷','悪堕ち',
        '精神崩壊','セックス','中出し','顔射','イラマチオ','フェラ','パイズリ','手コキ','潮吹き','絶頂','アヘ顔','全裸','乳首',
        'ペニス','ヴァギナ','クリトリス','近親','触手','レイプ','調教','スカトロ','ふたなり','yande',]; 

        function checkKeywords(text, keywords) {
          return keywords.some(k => {
            const key = k.toLowerCase();
            // 所有关键词都用 includes 匹配
            return text.includes(key);
          });
        }        

        const isR18Page = window.location.pathname === '/r18';

        let validCount = 0;

        for (const item of data) {
          const textToCheck = ((item.caption || '') + ' ' + (item.tags || '')).toLowerCase();
          
          let isHidden = false;
          if (isR18Page) {
             if (!checkKeywords(textToCheck, r18Keywords)) isHidden = true; 
          } else {
             if (hideR18 && checkKeywords(textToCheck, blockKeywords)) isHidden = true;
          }

          if (isHidden) continue;

          validCount++;

          if (offset === 0 && validCount === 1) {
              bgLayer.style.backgroundImage = \`url(/image/\${item.file_name})\`;
              bgLayer.style.opacity = '1';
          }
          // ================================

         const w = item.width || 3;
         const h = item.height || 4;
         const title = (item.caption || '').split('\\\\n')[0]; 

         let minH = colHeights[0];
         let minIdx = 0;
         for(let i=1; i<colCount; i++) {
           if(colHeights[i] < colHeights[minIdx]) {
             minIdx = i;
           }
         }

          const card = document.createElement('div');
          card.className = 'card';
          card.innerHTML = \`
            <a href="/detail/\${item.id}">
              <div class="card-inner">
                <div class="placeholder" style="--w:\${w};--h:\${h};"></div>
                <img class="card-img" src="/image/\${item.file_name}" loading="lazy" onload="this.classList.add('loaded')">
                <div class="meta"><div class="title">\${title}</div></div>
              </div>
            </a>\`;
          
            cols[minIdx].appendChild(card);
            const aspectRatio = (h / w) || 1.2; 
            colHeights[minIdx] += aspectRatio;
        }
        
        offset += data.length;
        
        if (validCount < 5 && data.length >= 20) {
          console.log(\`Page filtered (valid: \${validCount}/\${data.length}), auto loading next page...\`);              
            isLoading = false;
            setTimeout(() => load(false), 100); 
            return;
        }
      } catch (e) { console.error(e); }
      isLoading = false;
      tip.style.opacity = '0';
    }

    function doSearch(val) {
      q = val;
      load(true);
    }

    window.addEventListener('scroll', () => {
      if ((window.innerHeight + window.scrollY) >= document.body.offsetHeight - 1200) {
        load();
      }
    });

    load(true);
  </script>
</body>
</html>`;
}
