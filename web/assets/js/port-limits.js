const app = new Vue({
 el:'#app',delimiters:['[[',']]'],
 data:{themeSwitcher,rows:[],interfaces:[],search:'',loading:false,saving:false,visible:false,error:'',available:true,form:{},
 columns:[{title:'端口',dataIndex:'port'},{title:'备注',dataIndex:'remark'},{title:'网卡',dataIndex:'interface'},
 {title:'上传上限',dataIndex:'upload',scopedSlots:{customRender:'rate'}},{title:'下载上限',dataIndex:'download',scopedSlots:{customRender:'rate'}},
 {title:'状态',scopedSlots:{customRender:'state'}},{title:'操作',scopedSlots:{customRender:'actions'}}]},
 computed:{filtered(){const q=this.search.trim().toLowerCase();return this.rows.filter(r=>[r.port,r.remark].some(v=>String(v||'').toLowerCase().includes(q)));}},
 methods:{
  async refresh(){if(this.loading)return;this.loading=true;try{const {data}=await axios.get('panel/api/inbounds/portLimits',{timeout:15000});if(!data.success)throw Error(data.msg||'读取失败');this.rows=data.obj.rows;this.interfaces=data.obj.interfaces;this.available=data.obj.available;this.error=data.obj.error||'';}catch(e){this.error=e.message||'读取失败，请重新登录';}finally{this.loading=false;}},
  edit(row){this.form={...row};if(!this.form.interface)this.form.interface=this.interfaces[0];this.visible=true;},
  async submit(form){if(this.saving)return;this.saving=true;try{const {data}=await axios.post('panel/api/inbounds/portLimits',{id:form.id,port:form.port,interface:form.interface,upload:form.upload,download:form.download},{timeout:120000});if(!data.success)throw Error(data.msg||'设置失败');this.visible=false;this.$message.success('限速规则已更新');await this.refresh();}catch(e){this.error=e.message||'设置失败';this.$message.error(this.error);}finally{this.saving=false;}},
  save(){if(!this.form.interface||!Number.isInteger(this.form.upload)||!Number.isInteger(this.form.download)){this.$message.error('请选择网卡，速度请填整数');return;}return this.submit(this.form);},
  remove(row){return this.submit({...row,upload:0,download:0});}
 },mounted(){this.refresh();}
});
