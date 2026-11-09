import { test } from 'node:test';
import assert from 'node:assert/strict';
import https from 'node:https';
import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { chromium } from 'playwright';
import { execute } from './client.mjs';

test('Browser login, saved session, renewal, CSRF, retry key, HTML rejection and read-only check', async () => {
 const dir = await mkdtemp(join(tmpdir(), 'time-browser-'));
 let server;
 try {
  execFileSync('openssl', ['req','-x509','-newkey','rsa:2048','-nodes','-keyout',join(dir,'key'),'-out',join(dir,'cert'),'-days','1','-subj','/CN=localhost'],{stdio:'ignore'});
  let origin, authOrigin, generation=1, logins=0, bookings=0, calls=0, failOnce=false, html=false, expireOnPost=false;
  const keys=[];
  server=https.createServer({key:await readFile(join(dir,'key')),cert:await readFile(join(dir,'cert'))},async(req,res)=>{
   res.setHeader('Content-Type','text/html');
   const json=(status,data)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(data))};
   const redirect=location=>{res.writeHead(302,{Location:location});res.end()};
   const valid=req.headers.cookie?.includes(`session=${generation}`);
   if(req.url==='/api/session')return valid?json(200,{user:{username:'alice'},csrfToken:`csrf-${generation}`}):json(401,{});
   if(req.url==='/login')return redirect(authOrigin+'/identify');
   if(req.url==='/identify'){res.end('<form action="/password"><input name="uid" autocomplete="username"><button type="submit">Continue</button></form>');return;}
   if(req.url.startsWith('/password')){assert.ok(req.url.includes('uid=alice'));res.end('<form method="post" action="/finish"><input name="password" type="password"><button type="submit">Sign in</button></form>');return;}
   if(req.url==='/finish'){let body='';for await(const x of req)body+=x;assert.equal(body,'password=test-password');return redirect(origin+'/auth/callback');}
   if(req.url==='/auth/callback'){logins++;res.setHeader('Set-Cookie',`session=${generation}; Path=/; HttpOnly; Secure; SameSite=Lax`);res.end('Signed in');return;}
   if(req.url==='/api/post-time'){
    calls++; if(expireOnPost){expireOnPost=false;generation++;return json(401,{})} if(!valid)return json(401,{});
    assert.equal(req.headers['x-csrf-token'],`csrf-${generation}`);
    keys.push(req.headers['idempotency-key']);
    let body='';for await(const x of req)body+=x;
    assert.deepEqual(JSON.parse(body),{status:'Start',inputValue:'work'});
    if(html){res.writeHead(200,{'Content-Type':'text/html'});res.end('<html>Login</html>');return;}
    if(failOnce){failOnce=false;return json(503,{})}
    bookings++;return json(200,{message:'Status gewechselt'});
   }
   res.writeHead(404);res.end();
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const port=server.address().port;origin=`https://127.0.0.1:${port}`;authOrigin=`https://localhost:${port}`;
  // Test-only trust exception for a disposable self-signed loopback fixture.
  const browserType={async launch(options){const b=await chromium.launch(options);return {close:()=>b.close(),newContext:options=>b.newContext({...options,ignoreHTTPSErrors:true})}}};
  const cfg={origin,authOrigin,username:'alice',password:'test-password',sessionFile:join(dir,'session.json'),action:'check'};
  await execute({...cfg},browserType);assert.equal(logins,1);assert.equal(calls,0);
  assert.equal((await stat(cfg.sessionFile)).mode&0o777,0o600);
  const book={...cfg,action:'start_work',status:'Start',task:'work',key:'stable-key'};
  failOnce=true;await execute({...book},browserType);assert.equal(logins,1);assert.equal(bookings,1);assert.deepEqual(keys,['stable-key','stable-key']);
  generation++;await execute({...cfg},browserType);assert.equal(logins,2);assert.equal(bookings,1);
  expireOnPost=true;await execute({...book},browserType);assert.equal(logins,3);assert.equal(bookings,2);
  html=true;await assert.rejects(execute({...book},browserType),/Booking rejected/);assert.equal(bookings,2);
 } finally {if(server)await new Promise(resolve=>server.close(resolve));await rm(dir,{recursive:true,force:true});}
});
