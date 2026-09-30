import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {ts,visit,project} from '../../tools/project.mjs';

// Runs in the normal pnpm test/check pipeline. A new panel must use the shared
// transition owner; a new control must say whether it opens, selects or toggles.
function audit(source){
 const file=ts.createSourceFile('ui.ts',source,ts.ScriptTarget.Latest,true),errors=[];
 visit(file,node=>{
  if(ts.isStringLiteralLike(node)&&node.text.startsWith('window:'))errors.push('ambiguous window action');
  if(ts.isBinaryExpression(node)&&ts.isIdentifier(node.left)&&node.left.text==='panel'&&node.operatorToken.kind>=ts.SyntaxKind.FirstAssignment&&node.operatorToken.kind<=ts.SyntaxKind.LastAssignment){
   let owner=node.parent;while(owner&&!ts.isFunctionLike(owner))owner=owner.parent;
   if(!owner?.name||!['setPanel','resetPanel'].includes(owner.name.text))errors.push('panel assignment bypasses transition owner');
  }
  // 'warm' is the unseen first build of a window (window-warm.ts): no hooks or resets.
  if(ts.isCallExpression(node)&&ts.isIdentifier(node.expression)&&node.expression.text==='setPanel'){
   const intent=node.arguments[1];
   if(node.arguments.length<1||node.arguments.length>2||intent&&(!ts.isStringLiteral(intent)||!['open','toggle','select','warm'].includes(intent.text)))errors.push('invalid panel intent');
  }
 });return errors;
}

test('all UI navigation uses explicit actions and one transition owner',()=>{
 for(const [name,file]of project().files)if(name.startsWith('src/engine/runtime/ui/'))assert.deepEqual(audit(file.text),[],name);
});
for(const source of [
 'function activate(){panel="Shop";}',
 'function sidebar(){return "window:Inventory";}',
 'function select(){setPanel("Inventory","maybe-toggle");}',
])test('navigation guard rejects '+source,()=>assert.ok(audit(source).length));

test('open is the default and toggle remains an explicit opt-in',()=>{
 const source=readFileSync('src/engine/runtime/ui/ui.ts','utf8');
 const file=ts.createSourceFile('ui.ts',source,ts.ScriptTarget.Latest,true);let setter;
 visit(file,node=>{if(ts.isFunctionDeclaration(node)&&node.name?.text==='setPanel')setter=node;});
 assert.equal(setter?.parameters[1]?.initializer?.text,'open');
});
